package genesis

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"
	log "github.com/sirupsen/logrus"

	"github.com/docker/go-connections/nat"
	"github.com/jrcasso/gograph"
)

// This is the pattern for a CI system:
//
// Nodes are updated to possess a completion state: success/failure
// Each represents a container that needs to run during the build, defined by a DAG-defined dependency tree.
// Every cycle, the DAG is traversed and the state of each node's container is assessed.
// New containers that meet initialization conditions are then dispatched.
// If the containers exited for any other reason than 0, then the node status will reflect this with failure,
// and the dependent nodes will not dispatch, and will be given completion state: skipped.
// When all node completion states are no longer pending, the build completes.

// Pipeline represents one CI pipeline, which has many steps
type Pipeline struct {
	Name  string `yaml:"name"`
	Mount string `yaml:"mount,omitempty"`
	Steps []Step `yaml:"steps"`
}

// Step represents A CI pipeline step
type Step struct {
	Name        string   `yaml:"name"`
	Image       string   `yaml:"image"`
	Command     string   `yaml:"command,omitempty"`
	DependsOn   []string `yaml:"depends_on,flow,omitempty"`
	Environment []string `yaml:"environment,flow,omitempty"`
}

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

func (p Pipeline) ExtractGraph() gograph.DirectedGraph {
	// Instantiate an empty directed graph, initially without edges
	var graph = gograph.DirectedGraph{DirectedNodes: []*gograph.DirectedNode{}, RootDirectedNode: nil}
	var rootOverride = false

	for _, s := range p.Steps {
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
	for _, s := range p.Steps {
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

func (p Pipeline) TransitionStep(ctx context.Context, cli client.Client, node *gograph.DirectedNode) {
	switch node.Values["state"] {
	case WAITING:
		var shouldCancel = false
		var shouldDispatch = true
		for _, parent := range node.Parents {
			if parent.Values["state"] == FAILED || parent.Values["state"] == CANCELLED {
				shouldCancel = true
				break
			}
			if parent.Values["state"] != SUCCEEDED {
				shouldDispatch = false
				break
			}
		}
		if shouldCancel {
			log.Debugf("Cancelling %+v\n", node.Values["name"])
			node.Values["state"] = CANCELLED
			defer RemoveContainer(ctx, cli, node.Values["container"])
		}
		if shouldDispatch {
			log.Debugf("Dispatching %+v\n", node.Values["name"])
			if dispatch(ctx, cli, p, node) {
				node.Values["state"] = RUNNING
			} else {
				node.Values["state"] = FAILED
			}
		}
	case RUNNING:
		var container, err = cli.ContainerInspect(ctx, node.Values["container"])
		if err != nil {
			log.Warnf("Unable to inspect container %+v!", container.ID[:12])
		}

		if container.State.Status == "exited" {
			log.Debugf("Container %+v has exited with code %d", container.ID[:12], container.State.ExitCode)
			if container.State.ExitCode == 0 {
				node.Values["state"] = SUCCEEDED
			} else {
				node.Values["state"] = FAILED
			}
			RetreiveContainerLogs(cli, container.ID)
			defer RemoveContainer(ctx, cli, container.ID)
		}
	case CANCELLED:
	case SUCCEEDED:
	case FAILED:
	default:
		log.Fatal("Invalid state provided!")
	}
}

// CreateNewContainer Creates a new container
func CreateNewContainer(ctx context.Context, cli client.Client, image string, c chan string) bool {
	hostBinding := nat.PortBinding{
		HostIP:   "0.0.0.0",
		HostPort: "8000",
	}
	containerPort, err := nat.NewPort("tcp", "80")
	if err != nil {
		log.Fatal("Unable to get the port")
	}

	portBinding := nat.PortMap{containerPort: []nat.PortBinding{hostBinding}}
	cont, err := cli.ContainerCreate(
		ctx,
		&container.Config{
			Image: image,
		},
		&container.HostConfig{
			PortBindings: portBinding,
		}, nil, "")
	if err != nil {
		log.Fatal(err)
	}

	err = cli.ContainerStart(ctx, cont.ID, types.ContainerStartOptions{})
	c <- cont.ID
	return err == nil
}

// RemoveContainer removes containers after a step finishes
func RemoveContainer(ctx context.Context, cli client.Client, id string) {
	var removeOptions = types.ContainerRemoveOptions{
		RemoveVolumes: true,
		RemoveLinks:   false,
		Force:         true,
	}

	_, err := cli.ContainerInspect(ctx, id)
	if err != nil {
		log.Fatal(err)
	}

	log.Debugf("Removing container %+v\n", id[:12])

	err = cli.ContainerRemove(ctx, id, removeOptions)
	if err != nil {
		log.Fatal(err)
	}
}

// RetreiveContainerLogs gets the logs from the specified container
func RetreiveContainerLogs(cli client.Client, id string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var logOptions = types.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
	}

	reader, err := cli.ContainerLogs(ctx, id, logOptions)
	if err != nil {
		log.Fatal(err)
	}
	defer reader.Close()

	_, err = io.Copy(os.Stdout, reader)
	if err != nil && err != io.EOF {
		log.Fatal(err)
	}
}

// dispatch Creates a new container
func dispatch(ctx context.Context, cli client.Client, conf Pipeline, node *gograph.DirectedNode) bool {
	var cmd = strings.Fields(node.Values["command"])
	var config *container.Config

	if len(cmd) != 0 {
		config = &container.Config{Image: node.Values["image"], Cmd: cmd}
	} else {
		config = &container.Config{Image: node.Values["image"]}
	}
	hostBinding := nat.PortBinding{HostIP: "0.0.0.0", HostPort: "8000"}
	containerPort, err := nat.NewPort("tcp", "80")
	if err != nil {
		panic(err)
	}
	path, err := os.Getwd()
	if err != nil {
		panic(err)
	}

	portBinding := nat.PortMap{containerPort: []nat.PortBinding{hostBinding}}

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
					Type:   mount.TypeBind,
					Source: path,
					Target: "/genesis",
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
		return false
	}

	fmt.Printf("Dispatched %+v step container with ID %+v\n", node.Values["name"], cont.ID[:12])
	node.Values["container"] = cont.ID
	return true
}
