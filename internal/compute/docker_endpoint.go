package compute

import (
	"errors"
	"fmt"
	"regexp"
)

// ErrBadDockerEndpoint is returned for a Docker endpoint that must not reach
// the docker command line.
var ErrBadDockerEndpoint = errors.New("docker endpoint not accepted")

// dockerEndpointRe is an endpoint that is not shaped like an option and holds
// no control character.
var dockerEndpointRe = regexp.MustCompile(`^[^-\x00-\x1f\x7f][^\x00-\x1f\x7f]*$`)

// dockerHostArgs returns the --host arguments for a node's Docker endpoint,
// none when the endpoint is empty.
func dockerHostArgs(endpoint string) ([]string, error) {
	if endpoint == "" {
		return []string{}, nil
	}
	if !dockerEndpointRe.MatchString(endpoint) {
		return nil, fmt.Errorf("%w: %q starts with '-' or holds a control character", ErrBadDockerEndpoint, endpoint)
	}
	return []string{"--host", endpoint}, nil
}
