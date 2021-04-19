# Genesis: A lightweight, container-native CI System

&emsp;&emsp;![Go Version](https://img.shields.io/badge/Go-v1.15.8-blue)
&emsp;&emsp;![](https://img.shields.io/github/issues/jrcasso/genesis)
&emsp;&emsp;![GitHub release (latest SemVer including pre-releases)](https://img.shields.io/github/v/release/jrcasso/genesis?include_prereleases)

## Summary

Genesis is a lightweight, container-native continuous integration system written in Go. Leveraging the Docker engine, this program will execute configured pipelines locally, even supporting parallelism.

## Usage

### Example

The `.genesis.yml` YAML configuration for Genesis follows this syntax:
```yaml
name: maiden-voyage
mount: /Users/jcasso/Documents/code/personal/genesis
steps:
  - name: NodeA
    image: jrcasso/genesis:sleep

  - name: NodeB
    image: jrcasso/genesis:sleep
    depends_on: ["NodeA"]
    command: /bin/touch /genesis/foobar

  - name: NodeC
    image: python:latest
    command: pip install boto3
    depends_on: ["NodeA", "NodeB"]
```
### Pipeline Specification
| Field | Specification | Description |
| -------------| ------------- | ------------- |
|`name` | Required | A unique name for the current pipeline |
|`steps` | Required | A list of pipeline steps |
|`mount` | Optional |A path to a directory to bind as a volume in pipeline steps (*default*: current working directory) |

### Step Specification
| Field | Specification | Description |
| -------------| ------------- | ------------- |
|`name` | Required | A unique name for the step in this pipeline |
|`image` | Required |A Docker image that the current step will run inside |
|`command` | Optional | A command to run in the container (*default*: `CMD` command for the container) |
|`depends_on` | Optional | A list of pipeline steps upon which the current step depends on. The step will be dispatched as soon as step dependencies have exited successfully. If step dependencies fail or are cancelled, the step will be skipped (*default*: no dependencies)|



## Design

Genesis converts a `.genesis.yml` file into a directed, acyclic execution graph using graph theory primitives from github.com/jrcasso/gograph. This pipeline execution graph is then [topologically sorted](https://en.wikipedia.org/wiki/Topological_sorting) for handoff to the main lifecycle loop. This lifecycle loop of the pipeline is then managed by a [finite-state machine](https://en.wikipedia.org/wiki/Finite-state_machine) that both dispatches and inspects running containers.

This project endeavors to implement the following:
 - a functional, useful CI pipeline that leverages deterministic finite-state machine management
 - a demonstration of the functionalities it provides via CI configuration fixtures
 - a semantically versioned progression of package improvements

# Development Setup

Ensure you have the following prerequisites satisfied:
 - Docker for Desktop
 - VS Code Extensions: Remote Containers
   - Download and install Microsoft's VS Code extension for developing in [Remote Containers](vscode:extension/ms-vscode-remote.remote-containers)

>Note: This is a VS Code Remote Containers development project: all development is done within a container to reduce initial time-to-develop. Getting this project up and running on your machine can be as simple as pulling down the repository, running the Docker daemon the host machine, opening the project in VS Code, and clicking twice.


## Directions

- Clone the repository

```sh
git clone git@github.com:jrcasso/genesis
```

- Open the repository in VS Code
```sh
code genesis
```

- Ensure your Docker daemon is running and listening on `/var/run/docker.sock`
- In the bottom-left corner of the VS Code window, click the highlighted "><" button (or navigate to the Remote Containers extension).
- From the dropdown, select "Remote Containers: Reopen in Container"

That's it.

## Development Details

VS Code will begin to build an image that is specified in `.devcontainer/`; it will be the container image that you develop in. When it's done, it'll automatically throw your entire VS Code interface/environment inside that container where you may begin deveopment. The current configuration will also mount your Docker engine socket into this container, so that Docker commands may be issued from within to manage containers on the host. Utilitarian tools like git and all the things needed to run a Go program are in that environment. It's still a container, so all of the idempotency and innate destructivity of containers are in fact *features* of this development strategy. If everyone develops in the same way, the time-to-develop becomes incredibly small. This model, of course, fails for large projects - but this is not a large project. In any case, one can simply add Docker composition orchestration when that time comes.

Additional tooling that might be needed can be done so during container runtime; however, if it is something that should stick around for every other developer too (i.e. they might also run into this same issue), please modify the `.devcontainer/Dockerfile` and open a pull request.

Launch configurations have been created to facilitate debugging with VS Code's native debugger for both normal script execution as well as tests.
