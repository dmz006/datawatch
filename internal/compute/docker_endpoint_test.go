package compute

import (
	"context"
	"errors"
	"testing"
)

type recordingRunner struct{ calls int }

func (r *recordingRunner) Run(context.Context, string, ...string) (string, error) {
	r.calls++
	return "27.1.1", nil
}

func dockerNetworkNode(endpoint string) *Node {
	return &Node{
		Name: "n", Kind: KindDocker, Routing: RoutingDockerNetwork,
		RoutingDockerNetwork: &RoutingDockerNetworkConfig{Image: "i", DockerEndpoint: endpoint},
	}
}

// A Docker endpoint comes from a request. One that is shaped like an option or
// carries control characters never reaches the docker command line.
func TestDockerVersionRefusesUnsafeEndpoint(t *testing.T) {
	for _, ep := range []string{"--config=/tmp/x", "-H", "tcp://a\nb", "tcp://a\x00b", "unix:///s\x7f"} {
		for _, n := range []*Node{dockerNetworkNode(ep), {Name: "n", Kind: KindDocker, Docker: &DockerConfig{Endpoint: ep}}} {
			r := &recordingRunner{}
			if _, err := (&VersionProbe{Runner: r}).ProbeVersion(context.Background(), n); err == nil {
				t.Errorf("endpoint %q: want an error", ep)
			}
			if r.calls != 0 {
				t.Errorf("endpoint %q: docker was run", ep)
			}
		}
	}
}

func TestDockerVersionAcceptsRealEndpoints(t *testing.T) {
	for _, ep := range []string{
		"unix:///var/run/docker.sock", "tcp://172.17.0.1:2375", "ssh://ops@host.example:22",
		"npipe:////./pipe/docker_engine", "fd://", "127.0.0.1:2375", ":2375", "unix:///home/a b/docker.sock",
	} {
		r := &recordingRunner{}
		if _, err := (&VersionProbe{Runner: r}).ProbeVersion(context.Background(), dockerNetworkNode(ep)); err != nil || r.calls != 1 {
			t.Errorf("endpoint %q: err %v, calls %d", ep, err, r.calls)
		}
	}
}

// The reachability probes and the container lifecycle take the same endpoint.
func TestDockerProbesRefuseUnsafeEndpoint(t *testing.T) {
	const ep = "--config=/tmp/x"
	ctx := context.Background()
	if err := probeDocker(ctx, &Node{Docker: &DockerConfig{Endpoint: ep}}); !errors.Is(err, ErrBadDockerEndpoint) {
		t.Errorf("probeDocker: %v", err)
	}
	if err := probeDockerNetwork(ctx, dockerNetworkNode(ep)); !errors.Is(err, ErrBadDockerEndpoint) {
		t.Errorf("probeDockerNetwork: %v", err)
	}
}
