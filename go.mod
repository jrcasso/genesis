module github.com/jrcasso/genesis

replace github.com/jrcasso/gograph => /workspaces/gograph/

go 1.15

require (
	github.com/docker/distribution v2.7.1+incompatible // indirect
	github.com/docker/docker v1.13.1
	github.com/docker/go-connections v0.4.0
	github.com/docker/go-units v0.4.0 // indirect
	github.com/jrcasso/gograph v0.18.0
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	golang.org/x/net v0.0.0-20201031054903-ff519b6c9102 // indirect
	gopkg.in/yaml.v2 v2.4.0
)
