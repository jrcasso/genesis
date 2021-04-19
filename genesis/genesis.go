package genesis

import (
	"context"
	"io"
	"os"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	log "github.com/sirupsen/logrus"
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
