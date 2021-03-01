package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	"github.com/jrcasso/genesis/genesis"
	"github.com/jrcasso/gograph"
	"gopkg.in/yaml.v2"
)

// These represent valid step states
// We could use `iota`, but node values (and therefore state) are strings by default
// so it would require lots of strconv.Itoa(state) cals
const (
	WAITING   = "WAITING"
	RUNNING   = "RUNNING"
	DISPATCH  = "DISPATCH"
	CANCELLED = "CANCELLED"
	SUCCEEDED = "SUCCEEDED"
	FAILED    = "FAILED"
)

func validateConfiguration(config []byte) bool {
	return true
}

func readFile(path string) []byte {
	// Load YAML file
	configBytes, err := ioutil.ReadFile(path)
	if err != nil {
		log.Fatalf("error: %v", err)
	}
	fmt.Print(string(configBytes))
	return configBytes
}

func check(e error) {
	if e != nil {
		panic(e)
	}
}

func unmarshalConfigYaml(config []byte) genesis.Pipeline {
	t := genesis.Pipeline{}
	err := yaml.Unmarshal(config, &t)
	if err != nil {
		log.Fatalf("error: %v", err)
	}
	return t
}

func convertConfigToGraph(config genesis.Pipeline) gograph.DirectedGraph {
	// We're going to instantiate an empty directed graph, then add directed nodes to this graph
	// But the nodes will not have direction, yet!
	var graph = gograph.DirectedGraph{DirectedNodes: []*gograph.DirectedNode{}, RootDirectedNode: nil}
	var rootOverride = false

	for _, s := range config.Steps {
		if s.Name == "root" {
			rootOverride = true
		}
		var newNode = &gograph.DirectedNode{
			Parents:  []*gograph.DirectedNode{},
			Children: []*gograph.DirectedNode{},
			Values: map[string]string{
				"name":      s.Name,
				"image":     s.Image,
				"container": "",
				"command":   s.Command,
				"state":     WAITING,
			},
			ID: gograph.CreateDirectedNodeID(),
		}
		graph.DirectedNodes = append(graph.DirectedNodes, newNode)
		// One optimization is to try to set directed edge from a node to its dependency *here*. If it fails, put the step
		// in an array for processing in a block after this routine. In this follow-up block, *all* node dependencies are guaranteed
		// to exist (by virtue of this node instantiation), but we're able to take advantage of on-the-fly edge creation
		// in this loop! The next bock would simply fill in the edges where on-the-fly couldn't create them because a node didn't exist
	}

	// Add default root node if one was not specified
	if !rootOverride {
		var newRootNode = &gograph.DirectedNode{
			Parents:  []*gograph.DirectedNode{},
			Children: []*gograph.DirectedNode{},
			Values: map[string]string{
				"name":  "root",
				"image": "jrcasso/genesis:sleep",
				"state": WAITING,
			},
			ID: gograph.CreateDirectedNodeID(),
		}
		graph.DirectedNodes = append(graph.DirectedNodes, newRootNode)
		graph.RootDirectedNode = newRootNode
	}

	// Make the root node the parent of all other nodes, and all other nodes children of the root
	// node. We can't start CI without bootstrapping the first step.
	for _, node := range graph.DirectedNodes {
		if node.ID != graph.RootDirectedNode.ID {
			graph, _, _ = gograph.CreateDirectedEdge(graph, graph.RootDirectedNode, node)
		}
	}

	// Now let's process the steps again, this time assigning directed edges. We couldn't do this
	// before because we had no guarantee that a given node dependency had been initialized.
	for _, s := range config.Steps {
		for _, dependency := range s.DependsOn {
			var childNode = gograph.FindNodesByValues(graph, map[string]string{"name": s.Name})
			var parentNode = gograph.FindNodesByValues(graph, map[string]string{"name": dependency})
			if childNode == nil {
				panic(fmt.Sprintf("Node not found: '%+v'. Did you spell step names and dependencies correctly?", s.Name))
			}
			if parentNode == nil {
				panic(fmt.Sprintf("Node not found: '%+v'. Did you spell step names and dependencies correctly?", dependency))
			}
			if len(parentNode) > 1 || len(childNode) > 1 {
				panic("Multiple nodes with the same name found!")
			}
			graph, _, _ = gograph.CreateDirectedEdge(graph, parentNode[0], childNode[0])
		}
	}

	return graph
}

func main() {
	// Iniitialize
	ctx := context.Background()
	cli, err := client.NewEnvClient()
	if err != nil {
		fmt.Println("Unable to create docker client")
		panic(err)
	}

	var config = unmarshalConfigYaml(readFile("./fixtures/.genesis.yml"))

	// TODO consolidate deep copy into TopologicalSort
	// See "github.com/jinzhu/copier"
	var graph = convertConfigToGraph(config)
	var graphCopy = convertConfigToGraph(config)
	var sortedWithEdges = []*gograph.DirectedNode{}

	for _, edgelessNode := range gograph.TopologicalSort(graph) {
		for _, node := range graphCopy.DirectedNodes {
			if node.Values["name"] == edgelessNode.Values["name"] {
				sortedWithEdges = append(sortedWithEdges, node)
			}
		}
	}

	var sortedByName = []string{}
	for _, node := range sortedWithEdges {
		sortedByName = append(sortedByName, node.Values["name"])
	}
	fmt.Printf("%+v\n", config)
	fmt.Printf("%+v\n", graph)
	fmt.Println(sortedByName)

	// TODO: Consolidate keepGoing and allNodesCompleted
	var keepGoing = true
	fmt.Printf("BEGIN\n")
	for keepGoing {
		var allNodesCompleted = true
		fmt.Printf("================================================\n")
		for _, node := range sortedWithEdges {
			transition(ctx, *cli, config, node)
			fmt.Printf("Step %+v has state %+v\n", node.Values["name"], node.Values["state"])
			if node.Values["state"] == SUCCEEDED || node.Values["state"] == FAILED || node.Values["state"] == CANCELLED {
				allNodesCompleted = allNodesCompleted && true
				// Move on to the next node
				continue
			} else {
				allNodesCompleted = allNodesCompleted && false
			}
		}
		time.Sleep(2 * time.Second)
		keepGoing = !allNodesCompleted
	}
	fmt.Printf("END\n")
}

// TODO transition is starting to get overloaded
func transition(ctx context.Context, cli client.Client, config genesis.Pipeline, node *gograph.DirectedNode) {
	switch node.Values["state"] {
	case WAITING:
		var shouldCancel = false
		var shouldDispatch = true
		for _, parent := range node.Parents {
			if parent.Values["state"] == FAILED {
				shouldCancel = true
				break
			}
			if parent.Values["state"] != SUCCEEDED {
				shouldDispatch = false
				break
			}
		}
		if shouldCancel {
			fmt.Printf("Cancelling %+v\n", node.Values["name"])
			node.Values["state"] = CANCELLED
		}
		if shouldDispatch {
			fmt.Printf("Dispatching %+v\n", node.Values["name"])
			if dispatch(ctx, cli, config, node) {
				node.Values["state"] = RUNNING
			} else {
				node.Values["state"] = FAILED
			}
		}
	case RUNNING:
		var container, err = cli.ContainerInspect(ctx, node.Values["container"])
		if err != nil {
			fmt.Printf("Unable to inspect docker container %+v\n", node.Values["container"])
			panic(err)
		}
		if container.State.Status == "exited" {
			if container.State.ExitCode == 0 {
				node.Values["state"] = SUCCEEDED
			} else {
				node.Values["state"] = FAILED
			}
		}
	case CANCELLED:
	case SUCCEEDED:
	case FAILED:
	default:
		panic("Invalid state provided!")
	}
}

func dispatch(ctx context.Context, cli client.Client, config genesis.Pipeline, node *gograph.DirectedNode) bool {
	// var c chan string = make(chan string)

	// Update the below function to accept an argument for the node
	// so we can correlate the running container with the step later
	var didCreate, id = createNewContainer(ctx, cli, config, node)
	if didCreate {
		fmt.Printf("Dispatched %+v step container with ID %+v\n", node.Values["name"], id[:12])
		node.Values["container"] = id
		return true
	}
	return false
}

// createNewContainer Creates a new container
func createNewContainer(ctx context.Context, cli client.Client, conf genesis.Pipeline, node *gograph.DirectedNode) (bool, string) {
	var cmd = strings.Fields(node.Values["command"])
	var config *container.Config

	if len(cmd) != 0 {
		config = &container.Config{Image: node.Values["image"], Cmd: cmd}
	} else {
		config = &container.Config{Image: node.Values["image"]}
	}
	hostBinding := nat.PortBinding{
		HostIP:   "0.0.0.0",
		HostPort: "8000",
	}
	containerPort, err := nat.NewPort("tcp", "80")
	if err != nil {
		panic("Unable to get the port")
	}

	portBinding := nat.PortMap{containerPort: []nat.PortBinding{hostBinding}}

	path, err := os.Getwd()
	if err != nil {
		log.Println(err)
	}
	if conf.Mount != "" {
		path = conf.Mount
	}

	cont, err := cli.ContainerCreate(
		ctx,
		config,
		&container.HostConfig{
			PortBindings: portBinding,
			Mounts: []mount.Mount{
				{
					Type:     mount.TypeBind,
					Source:   path,
					Target:   "/genesis",
					ReadOnly: true,
				},
			},
		},
		nil,
		node.ID,
	)
	if err != nil {
		panic(err)
	}

	err = cli.ContainerStart(ctx, cont.ID, types.ContainerStartOptions{})
	if err != nil {
		fmt.Println(err)
		return false, cont.ID
	}
	return true, cont.ID
}
