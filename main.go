package main

import (
	"context"
	"io/ioutil"
	"os"
	"time"

	"github.com/docker/docker/client"
	"github.com/jrcasso/genesis/genesis"
	"github.com/jrcasso/gograph"
	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
)

func init() {
	log.SetOutput(os.Stdout)
	log.SetLevel(log.DebugLevel)
}

func readFile(path string) []byte {
	configBytes, err := ioutil.ReadFile(path)
	if err != nil {
		panic(err)
	}
	return configBytes
}

func unmarshalConfigYaml(config []byte) genesis.Pipeline {
	t := genesis.Pipeline{}
	err := yaml.Unmarshal(config, &t)
	if err != nil {
		panic(err)
	}
	return t
}

func main() {
	// Iniitialize
	ctx := context.Background()
	cli, err := client.NewEnvClient()
	if err != nil {
		log.Fatal("Unable to create docker client")
	}

	var pipeline = unmarshalConfigYaml(readFile("./fixtures/.genesis.yml"))

	var graph = pipeline.ExtractGraph()
	var graphCopy = pipeline.ExtractGraph()
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
	log.Debugf("%+v\n", pipeline)
	log.Debugf("%+v\n", graph)
	log.Debugln(sortedByName)

	// TODO: Consolidate keepGoing and allNodesCompleted
	var keepGoing = true
	log.Debug("BEGIN\n")
	for keepGoing {
		var allNodesCompleted = true
		log.Debug("================================================\n")
		for _, node := range sortedWithEdges {
			pipeline.TransitionStep(ctx, *cli, node)
			log.Debugf("Step %+v has state %+v\n", node.Values["name"], node.Values["state"])
			if node.Values["state"] == genesis.SUCCEEDED || node.Values["state"] == genesis.FAILED || node.Values["state"] == genesis.CANCELLED {
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
	log.Debug("END\n")
}
