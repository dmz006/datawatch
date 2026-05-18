// Package compute — docker-network lifecycle management for BL318/BL319.
//
// DockerLifecycle manages LLM container lifecycle for docker-network routing.
// Uses docker CLI (not SDK) to avoid import bloat.

package compute

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// DockerLifecycle manages LLM container lifecycle for docker-network routing.
type DockerLifecycle struct{}

// EnsureRunning ensures the LLM container for node n is running.
// Returns the container's IP on the docker network + the port to use.
// Creates the network if it doesn't exist.
// Pulls the image if AutoPull is true and image is absent.
func (d *DockerLifecycle) EnsureRunning(ctx context.Context, n *Node) (ip string, port int, err error) {
	if n.RoutingDockerNetwork == nil {
		return "", 0, fmt.Errorf("docker lifecycle: node %q has no routing_docker_network config", n.Name)
	}
	cfg := n.RoutingDockerNetwork.effective()

	if err := d.EnsureNetwork(ctx, cfg.NetworkName, cfg.DockerEndpoint); err != nil {
		return "", 0, fmt.Errorf("docker lifecycle: ensure network: %w", err)
	}

	cname := containerName(n)

	running, _, statusErr := d.Status(ctx, n)
	if statusErr != nil {
		// Container may not exist yet — treat as not running.
		running = false
	}

	if !running {
		if !cfg.AutoStart {
			return "", 0, fmt.Errorf("docker lifecycle: container %q is not running and auto_start is disabled", cname)
		}

		// Optionally pull the image first.
		if cfg.AutoPull {
			pullArgs := d.hostArgs(cfg.DockerEndpoint)
			pullArgs = append(pullArgs, "pull", cfg.Image)
			out, perr := exec.CommandContext(ctx, "docker", pullArgs...).CombinedOutput()
			if perr != nil {
				return "", 0, fmt.Errorf("docker lifecycle: pull %q: %v — %s", cfg.Image, perr, strings.TrimSpace(string(out)))
			}
		}

		// Build docker run args.
		runArgs := d.hostArgs(cfg.DockerEndpoint)
		runArgs = append(runArgs,
			"run", "-d",
			"--name", cname,
			"--network", cfg.NetworkName,
			"--restart", "unless-stopped",
		)
		for _, e := range cfg.Env {
			runArgs = append(runArgs, "-e", e)
		}
		runArgs = append(runArgs, cfg.Image)

		out, runErr := exec.CommandContext(ctx, "docker", runArgs...).CombinedOutput()
		if runErr != nil {
			return "", 0, fmt.Errorf("docker lifecycle: run container %q: %v — %s", cname, runErr, strings.TrimSpace(string(out)))
		}
	}

	// Get the container's IP on the named network.
	inspectArgs := d.hostArgs(cfg.DockerEndpoint)
	inspectArgs = append(inspectArgs,
		"inspect",
		"--format", fmt.Sprintf(`{{(index .NetworkSettings.Networks "%s").IPAddress}}`, cfg.NetworkName),
		cname,
	)
	out, inspErr := exec.CommandContext(ctx, "docker", inspectArgs...).CombinedOutput()
	if inspErr != nil {
		return "", 0, fmt.Errorf("docker lifecycle: inspect container %q: %v — %s", cname, inspErr, strings.TrimSpace(string(out)))
	}
	containerIP := strings.TrimSpace(string(out))
	if containerIP == "" {
		return "", 0, fmt.Errorf("docker lifecycle: container %q has no IP on network %q", cname, cfg.NetworkName)
	}

	return containerIP, cfg.Port, nil
}

// EnsureNetwork creates the named docker bridge network if it doesn't exist.
func (d *DockerLifecycle) EnsureNetwork(ctx context.Context, networkName, endpoint string) error {
	args := d.hostArgs(endpoint)
	args = append(args, "network", "inspect", networkName)
	if err := exec.CommandContext(ctx, "docker", args...).Run(); err == nil {
		return nil // network already exists
	}
	// Create the network.
	createArgs := d.hostArgs(endpoint)
	createArgs = append(createArgs, "network", "create", "--driver", "bridge", networkName)
	out, err := exec.CommandContext(ctx, "docker", createArgs...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker network create %q: %v — %s", networkName, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Teardown stops and removes the container managed by node n. Errors are ignored
// to allow best-effort cleanup.
func (d *DockerLifecycle) Teardown(ctx context.Context, n *Node) error {
	cname := containerName(n)
	endpoint := ""
	if n.RoutingDockerNetwork != nil {
		endpoint = n.RoutingDockerNetwork.DockerEndpoint
	}

	stopArgs := d.hostArgs(endpoint)
	stopArgs = append(stopArgs, "stop", cname)
	_ = exec.CommandContext(ctx, "docker", stopArgs...).Run()

	rmArgs := d.hostArgs(endpoint)
	rmArgs = append(rmArgs, "rm", cname)
	_ = exec.CommandContext(ctx, "docker", rmArgs...).Run()

	return nil
}

// Status returns whether the container is running and its ID.
func (d *DockerLifecycle) Status(ctx context.Context, n *Node) (running bool, containerID string, err error) {
	cname := containerName(n)
	endpoint := ""
	if n.RoutingDockerNetwork != nil {
		endpoint = n.RoutingDockerNetwork.DockerEndpoint
	}

	args := d.hostArgs(endpoint)
	args = append(args, "inspect", "--format", `{{.State.Running}} {{.Id}}`, cname)
	out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	if err != nil {
		return false, "", fmt.Errorf("docker inspect %q: %v — %s", cname, err, strings.TrimSpace(string(out)))
	}
	parts := strings.SplitN(strings.TrimSpace(string(out)), " ", 2)
	if len(parts) < 2 {
		return false, "", fmt.Errorf("docker inspect %q: unexpected output %q", cname, strings.TrimSpace(string(out)))
	}
	running = parts[0] == "true"
	containerID = parts[1]
	return running, containerID, nil
}

// containerName returns the container name for node n.
// Uses RoutingDockerNetwork.ContainerName if set; otherwise "dw-" + n.Name.
func containerName(n *Node) string {
	if n.RoutingDockerNetwork != nil && n.RoutingDockerNetwork.ContainerName != "" {
		return n.RoutingDockerNetwork.ContainerName
	}
	return "dw-" + n.Name
}

// hostArgs returns the --host flag slice when endpoint is non-empty.
func (d *DockerLifecycle) hostArgs(endpoint string) []string {
	if endpoint != "" {
		return []string{"--host", endpoint}
	}
	return []string{}
}
