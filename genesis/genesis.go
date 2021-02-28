package genesis

import (
	"context"
	"fmt"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
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

// This is the pattern for a component testing system:
//
// A system evaluation cycle is repeated with frequency (e.g. 1 second). This

// Pipeline represents one CI pipeline, which has many steps
type Pipeline struct {
	Name  string `yaml:"name"`
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

// CreateNewContainer Creates a new container
func CreateNewContainer(ctx context.Context, cli client.Client, image string, c chan string) bool {
	hostBinding := nat.PortBinding{
		HostIP:   "0.0.0.0",
		HostPort: "8000",
	}
	containerPort, err := nat.NewPort("tcp", "80")
	if err != nil {
		panic("Unable to get the port")
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
		panic(err)
	}

	err = cli.ContainerStart(ctx, cont.ID, types.ContainerStartOptions{})
	c <- cont.ID
	if err != nil {
		return false
	}
	return true
}

// DeleteContainer adsa
func DeleteContainer(ctx context.Context, cli client.Client, id string) {
	var info types.ContainerJSON
	var removeOptions = types.ContainerRemoveOptions{
		RemoveVolumes: true,
		RemoveLinks:   false,
		Force:         true,
	}

	info, err := cli.ContainerInspect(ctx, id)
	fmt.Printf("%+v\n", info.ContainerJSONBase)

	err = cli.ContainerStop(ctx, id, nil)
	if err != nil {
		panic(err)
	}

	err = cli.ContainerRemove(ctx, id, removeOptions)
	if err != nil {
		panic(err)
	}
}

// ContainerList
func ContainerList(ctx context.Context, cli client.Client, id string) {

}
