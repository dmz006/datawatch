// v7.0.0-alpha.19 #245 — kind-aware save-time probes for ComputeNodes.
// Operator-spec'd 2026-05-09 Q3: "Yes for all kinds (slow but safe)".
// Save fails with the probe error if anything's unreachable. PWA Add
// popup propagates the error inline so operator can fix.
//
// Probes by kind:
//   local        — no probe (always OK)
//   ssh          — `ssh -o BatchMode=yes -o ConnectTimeout=5 [user@]host[:port] [-i keypath] echo ok`
//   docker       — `docker [--host=endpoint] info` (verifies daemon reachable)
//   k8s          — `kubectl --kubeconfig=<from-cluster-profile> get nodes -l <namespace>`
//   remote       — HTTP HEAD on Address with 5s timeout
//   remote-proxy — HTTP HEAD on Address + Authorization: Bearer <token>

package compute

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

// ClusterLookup is the interface the probe uses to resolve a k8s
// ClusterProfile by name. The server wires it; tests can pass a mock.
type ClusterLookup interface {
	// GetClusterProfile returns the profile's kubeconfig path + context
	// name + any extra kubectl flags. Returns ("", "", nil, error) when
	// the profile is missing.
	GetClusterProfile(name string) (kubeconfigPath, contextName string, extraArgs []string, err error)
}

// Probe runs the kind-appropriate connectivity check. Returns nil on
// success. Error message is operator-readable and includes the
// failing command's stderr when applicable.
//
// ctx must carry a reasonable deadline (e.g. 10s) — the probe
// honours it via exec.CommandContext + http.Request context.
func Probe(ctx context.Context, n *Node, clusters ClusterLookup) error {
	if n == nil {
		return fmt.Errorf("probe: nil node")
	}
	switch n.Kind {
	// alpha.23 supported kinds — both reach the LLM endpoint over HTTP.
	// Probe is identical: HEAD on n.Address verifies the host is up.
	// 4xx responses (404/405) still confirm reachability — we only care
	// about connection errors.
	case KindOllama, KindOpenAICompat:
		return probeHTTP(ctx, n, "")
	case KindLocal:
		return nil
	case KindSSH:
		return probeSSH(ctx, n)
	case KindDocker:
		return probeDocker(ctx, n)
	case KindK8s:
		return probeK8s(ctx, n, clusters)
	case KindRemote:
		return probeHTTP(ctx, n, "")
	case KindRemoteProxy:
		// remote-proxy might carry a token in Address (rare) or in a
		// future ProxyToken field. For now, treat same as remote.
		return probeHTTP(ctx, n, "")
	default:
		return fmt.Errorf("probe: unknown kind %q", n.Kind)
	}
}

func probeSSH(ctx context.Context, n *Node) error {
	if n.SSH == nil || strings.TrimSpace(n.SSH.Host) == "" {
		return fmt.Errorf("ssh probe: ssh.host required")
	}
	target := n.SSH.Host
	if n.SSH.User != "" {
		target = n.SSH.User + "@" + n.SSH.Host
	}
	args := []string{
		"-o", "BatchMode=yes",
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "ConnectTimeout=5",
	}
	if n.SSH.Port > 0 && n.SSH.Port != 22 {
		args = append(args, "-p", fmt.Sprintf("%d", n.SSH.Port))
	}
	if n.SSH.KeyPath != "" {
		args = append(args, "-i", n.SSH.KeyPath)
	}
	args = append(args, target, "echo", "ok")
	out, err := exec.CommandContext(ctx, "ssh", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("ssh probe failed: %v — %s", err, strings.TrimSpace(string(out)))
	}
	if !strings.Contains(string(out), "ok") {
		return fmt.Errorf("ssh probe: unexpected response %q", strings.TrimSpace(string(out)))
	}
	return nil
}

func probeDocker(ctx context.Context, n *Node) error {
	args := []string{}
	if n.Docker != nil && n.Docker.Endpoint != "" {
		args = append(args, "--host", n.Docker.Endpoint)
	}
	args = append(args, "info", "--format", "{{.ServerVersion}}")
	out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker probe failed: %v — %s", err, strings.TrimSpace(string(out)))
	}
	if strings.TrimSpace(string(out)) == "" {
		return fmt.Errorf("docker probe: empty server version response")
	}
	return nil
}

func probeK8s(ctx context.Context, n *Node, clusters ClusterLookup) error {
	if n.K8s == nil || strings.TrimSpace(n.K8s.ClusterProfile) == "" {
		return fmt.Errorf("k8s probe: k8s.cluster_profile required (operator-decision Q2: Cluster Profiles only)")
	}
	if clusters == nil {
		return fmt.Errorf("k8s probe: cluster registry unavailable")
	}
	kubeconfig, contextName, extra, err := clusters.GetClusterProfile(n.K8s.ClusterProfile)
	if err != nil {
		return fmt.Errorf("k8s probe: cluster profile %q: %v", n.K8s.ClusterProfile, err)
	}
	args := []string{}
	if kubeconfig != "" {
		args = append(args, "--kubeconfig", kubeconfig)
	}
	if contextName != "" {
		args = append(args, "--context", contextName)
	}
	if n.K8s.Namespace != "" {
		args = append(args, "--namespace", n.K8s.Namespace)
	}
	args = append(args, extra...)
	args = append(args, "get", "nodes", "-o", "name")
	out, err := exec.CommandContext(ctx, "kubectl", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("k8s probe failed: %v — %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func probeHTTP(ctx context.Context, n *Node, bearerToken string) error {
	if strings.TrimSpace(n.Address) == "" {
		return fmt.Errorf("http probe: address required")
	}
	addr := n.Address
	// HEAD against the address. Most APIs respond OK or 404; both are
	// "reachable". Connection refused / timeout are the failures we
	// care about.
	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // #nosec G402 -- operator-declared remote, often self-signed
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, addr, nil)
	if err != nil {
		return fmt.Errorf("http probe build: %v", err)
	}
	if bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+bearerToken)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("http probe: %v", err)
	}
	defer resp.Body.Close()
	// Any HTTP response means reachable. Even 401/403/404/405 confirm
	// the host is up.
	return nil
}
