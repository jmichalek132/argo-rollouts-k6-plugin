//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"sigs.k8s.io/e2e-framework/pkg/env"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/envfuncs"
	"sigs.k8s.io/e2e-framework/support/kind"
)

var (
	testenv    env.Environment
	mockServer *MockK6Server
)

func TestMain(m *testing.M) {
	testenv = env.New()
	kindClusterName := envconf.RandomName("k6-plugin", 16)
	namespace := envconf.RandomName("k6-e2e", 16)

	testenv.Setup(
		envfuncs.CreateCluster(kind.NewProvider(), kindClusterName),
		envfuncs.CreateNamespace(namespace),
		installArgoRollouts(),
		buildAndLoadPlugins(kindClusterName),
		startMockServerAndConfigure(kindClusterName),
	)
	testenv.Finish(
		stopMockServer(),
		envfuncs.DeleteNamespace(namespace),
		envfuncs.DestroyCluster(kindClusterName),
	)

	os.Exit(testenv.Run(m))
}

// installArgoRollouts installs Argo Rollouts CRDs and controller into the cluster.
func installArgoRollouts() env.Func {
	return func(ctx context.Context, cfg *envconf.Config) (context.Context, error) {
		log.Println("Installing Argo Rollouts...")

		// Create the argo-rollouts namespace.
		if err := runKubectl(cfg, "create", "namespace", "argo-rollouts"); err != nil {
			return ctx, fmt.Errorf("create argo-rollouts namespace: %w", err)
		}

		// Install Argo Rollouts from the official release manifest.
		if err := runKubectl(cfg, "apply", "-n", "argo-rollouts", "-f",
			"https://github.com/argoproj/argo-rollouts/releases/download/v1.9.0/install.yaml"); err != nil {
			return ctx, fmt.Errorf("install argo rollouts: %w", err)
		}

		// Wait for the controller to be ready.
		if err := runKubectl(cfg, "rollout", "status", "deployment/argo-rollouts",
			"-n", "argo-rollouts", "--timeout=120s"); err != nil {
			return ctx, fmt.Errorf("wait for argo-rollouts controller: %w", err)
		}

		log.Println("Argo Rollouts installed successfully")
		return ctx, nil
	}
}

// buildAndLoadPlugins cross-compiles both plugin binaries for linux/amd64 and
// copies them into the kind node at /tmp/argo-rollouts/.
func buildAndLoadPlugins(clusterName string) env.Func {
	return func(ctx context.Context, cfg *envconf.Config) (context.Context, error) {
		log.Println("Building plugin binaries for linux/amd64...")

		tmpDir, err := os.MkdirTemp("", "e2e-plugins")
		if err != nil {
			return ctx, fmt.Errorf("create temp dir: %w", err)
		}

		// Cross-compile both binaries.
		for _, binary := range []struct {
			name string
			path string
		}{
			{"metric-plugin", "./cmd/metric-plugin"},
			{"step-plugin", "./cmd/step-plugin"},
		} {
			outPath := filepath.Join(tmpDir, binary.name)
			cmd := exec.Command("go", "build", "-o", outPath, binary.path)
			cmd.Env = append(os.Environ(),
				"GOOS=linux",
				"GOARCH=amd64",
				"CGO_ENABLED=0",
			)
			if out, err := cmd.CombinedOutput(); err != nil {
				return ctx, fmt.Errorf("build %s: %w\n%s", binary.name, err, string(out))
			}
			log.Printf("Built %s -> %s", binary.name, outPath)
		}

		// Get the kind node name (typically <cluster>-control-plane).
		nodeName := clusterName + "-control-plane"

		// Create the target directory inside the kind node.
		if out, err := exec.Command("docker", "exec", nodeName,
			"mkdir", "-p", "/tmp/argo-rollouts").CombinedOutput(); err != nil {
			return ctx, fmt.Errorf("create plugin dir in kind node: %w\n%s", err, string(out))
		}

		// Copy binaries into the kind node.
		for _, name := range []string{"metric-plugin", "step-plugin"} {
			src := filepath.Join(tmpDir, name)
			dst := fmt.Sprintf("%s:/tmp/argo-rollouts/%s", nodeName, name)
			if out, err := exec.Command("docker", "cp", src, dst).CombinedOutput(); err != nil {
				return ctx, fmt.Errorf("docker cp %s: %w\n%s", name, err, string(out))
			}
			// Make binary executable.
			if out, err := exec.Command("docker", "exec", nodeName,
				"chmod", "+x", "/tmp/argo-rollouts/"+name).CombinedOutput(); err != nil {
				return ctx, fmt.Errorf("chmod %s: %w\n%s", name, err, string(out))
			}
			log.Printf("Loaded %s into kind node %s", name, nodeName)
		}

		// Apply the argo-rollouts-config ConfigMap with file:// paths.
		configPath := filepath.Join("e2e", "testdata", "argo-rollouts-config.yaml")
		if err := runKubectl(cfg, "apply", "-f", configPath); err != nil {
			return ctx, fmt.Errorf("apply argo-rollouts-config: %w", err)
		}

		log.Println("Plugin binaries loaded and ConfigMap applied")
		return ctx, nil
	}
}

// startMockServerAndConfigure starts the mock k6 API server and injects K6_BASE_URL
// into the argo-rollouts controller deployment so plugin binaries can reach it.
func startMockServerAndConfigure(clusterName string) env.Func {
	return func(ctx context.Context, cfg *envconf.Config) (context.Context, error) {
		log.Println("Starting mock k6 API server...")

		var err error
		mockServer, err = NewMockK6Server()
		if err != nil {
			return ctx, fmt.Errorf("start mock server: %w", err)
		}
		log.Printf("Mock k6 server listening on %s", mockServer.URL())

		// Discover Docker bridge gateway IP for host accessibility from kind cluster.
		gatewayIP := discoverGatewayIP(clusterName)
		log.Printf("Docker gateway IP: %s", gatewayIP)

		// Inject K6_BASE_URL into the argo-rollouts controller deployment.
		baseURL := fmt.Sprintf("http://%s:%d", gatewayIP, mockServer.Port())
		if err := runKubectl(cfg, "set", "env", "deployment/argo-rollouts",
			"-n", "argo-rollouts", fmt.Sprintf("K6_BASE_URL=%s", baseURL)); err != nil {
			return ctx, fmt.Errorf("inject K6_BASE_URL: %w", err)
		}

		// Wait for controller to restart with the new env var.
		if err := runKubectl(cfg, "rollout", "status", "deployment/argo-rollouts",
			"-n", "argo-rollouts", "--timeout=60s"); err != nil {
			return ctx, fmt.Errorf("wait for controller restart: %w", err)
		}

		log.Printf("K6_BASE_URL=%s injected into argo-rollouts controller", baseURL)
		return ctx, nil
	}
}

// stopMockServer shuts down the mock k6 API server.
func stopMockServer() env.Func {
	return func(ctx context.Context, _ *envconf.Config) (context.Context, error) {
		if mockServer != nil {
			mockServer.Close()
			log.Println("Mock k6 server stopped")
		}
		return ctx, nil
	}
}

// discoverGatewayIP finds the Docker bridge gateway IP for the kind network.
// This IP allows containers inside the kind cluster to reach the host machine.
func discoverGatewayIP(clusterName string) string {
	// Try to get the gateway from the kind network.
	out, err := exec.Command("docker", "network", "inspect", "kind",
		"-f", "{{(index .IPAM.Config 0).Gateway}}").CombinedOutput()
	if err == nil {
		ip := strings.TrimSpace(string(out))
		if ip != "" {
			return ip
		}
	}

	// Fallback: default kind network gateway.
	_ = clusterName // suppress unused warning
	return "172.18.0.1"
}

// runKubectl executes a kubectl command using the kubeconfig from the envconf.
func runKubectl(cfg *envconf.Config, args ...string) error {
	kubeconfig := cfg.KubeconfigFile()
	fullArgs := append([]string{"--kubeconfig", kubeconfig}, args...)
	cmd := exec.Command("kubectl", fullArgs...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Stdout = os.Stderr // log kubectl output to stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", stderr.String(), err)
	}
	return nil
}
