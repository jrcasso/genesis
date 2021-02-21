package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"sync"

	"github.com/docker/docker/client"
	"github.com/jrcasso/genesis/genesis"
	"github.com/jrcasso/gograph"
	"gopkg.in/yaml.v2"
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
				"name":    s.Name,
				"image":   s.Image,
				"command": s.Command,
				"state":   "pending",
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
				"image": "sverrirab/sleep",
				"state": "pending",
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

	// In graph theory, a topological sort or topological ordering of a directed acyclic graph (DAG)
	// is a linear ordering of its nodes in which each node comes before all nodes to which it has
	// outbound edges. Every DAG has one or more topological sorts. We attempt to sort the DAG here
	// to panic if it is not possible.

	var sortedNodes = gograph.TopologicalSort(graph)
	var sortedByName = []string{}
	for _, node := range sortedNodes {
		sortedByName = append(sortedByName, node.Values["name"])
	}
	fmt.Println(sortedNodes)
	fmt.Println(sortedByName)

	return graph
}

func main() {
	// Iniitialize
	var config = unmarshalConfigYaml(readFile("./fixtures/.genesis.yml"))
	var graph = convertConfigToGraph(config)

	fmt.Printf("%+v\n", config)
	// // The daemon process is the root node
	// graph, rootNode = gograph.CreateDirectedNode(graph, map[string]string{"foo": "bar"}, []*gograph.DirectedNode{}, []*gograph.DirectedNode{})
	// ptrNode = rootNode

	fmt.Printf("%+v\n", graph)
}

func dispatchContainers() {
	var c chan string = make(chan string)
	var wg sync.WaitGroup
	ctx := context.Background()

	cli, err := client.NewEnvClient()
	if err != nil {
		fmt.Println("Unable to create docker client")
		panic(err)
	}

	for i := 0; i < 1; i++ {
		wg.Add(1)
		go genesis.CreateNewContainer(ctx, *cli, "sverrirab/sleep", c, &wg)
	}

	var id = <-c
	fmt.Println("Waiting for containers to finish")
	wg.Wait()
	fmt.Println("Stopping containers")
	fmt.Println(id)
	genesis.DeleteContainer(ctx, *cli, id)
}
