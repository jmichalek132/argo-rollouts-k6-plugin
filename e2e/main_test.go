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
	"runtime"
	"strings"
	"testing"

	"sigs.k8s.io/e2e-framework/pkg/env"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/envfuncs"
	"sigs.k8s.io/e2e-framework/support/kind"
)

var testenv env.Environment

func TestMain(m *testing.M) {
	testenv = env.New()
	kindClusterName := envconf.RandomName("k6-plugin", 16)
	namespace := envconf.RandomName("k6-e2e", 16)

	setupFn := buildDeployPluginsAndMock(kindClusterName)
	if os.Getenv("K6_LIVE_TEST") == "true" {
		setupFn = buildDeployPluginsNoMock(kindClusterName)
	}

	testenv.Setup(
		envfuncs.CreateCluster(kind.NewProvider(), kindClusterName),
		envfuncs.CreateNamespace(namespace),
		installArgoRollouts(),
		setupFn,
	)
	testenv.Finish(
		envfuncs.DeleteNamespace(namespace),
		envfuncs.DestroyCluster(kindClusterName),
	)

	os.Exit(testenv.Run(m))
}

// installArgoRollouts installs Argo Rollouts CRDs and controller into the cluster.
func installArgoRollouts() env.Func {
	return func(ctx context.Context, cfg *envconf.Config) (context.Context, error) {
		log.Println("Installing Argo Rollouts...")

		if err := runKubectl(cfg, "create", "namespace", "argo-rollouts"); err != nil {
			return ctx, fmt.Errorf("create argo-rollouts namespace: %w", err)
		}

		if err := runKubectl(cfg, "apply", "-n", "argo-rollouts", "-f",
			"https://github.com/argoproj/argo-rollouts/releases/download/v1.9.0/install.yaml"); err != nil {
			return ctx, fmt.Errorf("install argo rollouts: %w", err)
		}

		if err := runKubectl(cfg, "rollout", "status", "deployment/argo-rollouts",
			"-n", "argo-rollouts", "--timeout=120s"); err != nil {
			return ctx, fmt.Errorf("wait for argo-rollouts controller: %w", err)
		}

		log.Println("Argo Rollouts installed successfully")
		return ctx, nil
	}
}

// buildDeployPluginsAndMock compiles all three binaries (metric-plugin, step-plugin, mock-server),
// packages them into a Docker image, loads the image into kind, deploys the mock-k6 service,
// and patches the argo-rollouts controller to load the plugin binaries from an emptyDir volume.
func buildDeployPluginsAndMock(clusterName string) env.Func {
	return func(ctx context.Context, cfg *envconf.Config) (context.Context, error) {
		goarch := runtime.GOARCH
		log.Printf("Building binaries for linux/%s...", goarch)

		// Tests run with CWD = e2e/; go up one level to reach the module root.
		moduleRoot, err := filepath.Abs("..")
		if err != nil {
			return ctx, fmt.Errorf("resolve module root: %w", err)
		}

		// Resolve GOPATH absolutely — the env may contain an un-expanded "$HOME/go".
		gopath := os.Getenv("GOPATH")
		if gopath == "" || !filepath.IsAbs(gopath) {
			home, _ := os.UserHomeDir()
			gopath = filepath.Join(home, "go")
		}
		baseEnv := make([]string, 0, len(os.Environ()))
		for _, e := range os.Environ() {
			if !strings.HasPrefix(e, "GOPATH=") {
				baseEnv = append(baseEnv, e)
			}
		}

		tmpDir, err := os.MkdirTemp("", "e2e-plugins")
		if err != nil {
			return ctx, fmt.Errorf("create temp dir: %w", err)
		}
		defer os.RemoveAll(tmpDir)

		// Compile all three binaries from the module root.
		for _, binary := range []struct {
			name string
			path string
		}{
			{"metric-plugin", "./cmd/metric-plugin"},
			{"step-plugin", "./cmd/step-plugin"},
			{"mock-server", "./e2e/mock"},
		} {
			outPath := filepath.Join(tmpDir, binary.name)
			cmd := exec.Command("go", "build", "-o", outPath, binary.path)
			cmd.Dir = moduleRoot
			cmd.Env = append(baseEnv, "GOOS=linux", "GOARCH="+goarch, "CGO_ENABLED=0", "GOPATH="+gopath)
			if out, err := cmd.CombinedOutput(); err != nil {
				return ctx, fmt.Errorf("build %s: %w\n%s", binary.name, err, string(out))
			}
			log.Printf("Built %s", binary.name)
		}

		// Write a Dockerfile packaging all three binaries.
		// Alpine provides sh/cp. Run as nobody (65534) to satisfy runAsNonRoot.
		dockerfile := "FROM alpine:3.19\n" +
			"COPY metric-plugin step-plugin mock-server /plugins/\n" +
			"RUN chmod 755 /plugins/metric-plugin /plugins/step-plugin /plugins/mock-server\n" +
			"USER 65534\n"
		if err := os.WriteFile(filepath.Join(tmpDir, "Dockerfile"), []byte(dockerfile), 0o644); err != nil {
			return ctx, fmt.Errorf("write Dockerfile: %w", err)
		}

		const imageName = "k6-plugin-binaries:e2e"
		if out, err := exec.Command("docker", "build", "-t", imageName, tmpDir).CombinedOutput(); err != nil {
			return ctx, fmt.Errorf("docker build: %w\n%s", err, string(out))
		}
		log.Printf("Built Docker image %s", imageName)

		if out, err := exec.Command("kind", "load", "docker-image", imageName, "--name", clusterName).CombinedOutput(); err != nil {
			return ctx, fmt.Errorf("kind load docker-image: %w\n%s", err, string(out))
		}
		log.Printf("Loaded %s into kind cluster %s", imageName, clusterName)

		// Apply the argo-rollouts-config ConfigMap with file:// plugin paths.
		configPath := filepath.Join(moduleRoot, "e2e", "testdata", "argo-rollouts-config.yaml")
		if err := runKubectl(cfg, "apply", "-f", configPath); err != nil {
			return ctx, fmt.Errorf("apply argo-rollouts-config: %w", err)
		}

		// Deploy mock-k6 as an in-cluster Deployment + Service.
		// Plugin binaries call K6_BASE_URL which resolves to this service — no host networking needed.
		const mockYAML = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: mock-k6
  namespace: argo-rollouts
spec:
  replicas: 1
  selector:
    matchLabels:
      app: mock-k6
  template:
    metadata:
      labels:
        app: mock-k6
    spec:
      containers:
      - name: mock-k6
        image: k6-plugin-binaries:e2e
        imagePullPolicy: Never
        command: ["/plugins/mock-server"]
        securityContext:
          runAsUser: 65534
        ports:
        - containerPort: 8080
---
apiVersion: v1
kind: Service
metadata:
  name: mock-k6
  namespace: argo-rollouts
spec:
  selector:
    app: mock-k6
  ports:
  - port: 8080
    targetPort: 8080
`
		if err := kubectlApplyStdin(cfg, mockYAML); err != nil {
			return ctx, fmt.Errorf("deploy mock-k6: %w", err)
		}

		if err := runKubectl(cfg, "rollout", "status", "deployment/mock-k6",
			"-n", "argo-rollouts", "--timeout=60s"); err != nil {
			return ctx, fmt.Errorf("wait for mock-k6: %w", err)
		}
		log.Println("mock-k6 service deployed")

		if err := patchArgoRolloutsController(cfg, "http://mock-k6.argo-rollouts.svc.cluster.local:8080"); err != nil {
			return ctx, err
		}

		log.Println("Plugin binaries loaded, mock server deployed, controller ready")
		return ctx, nil
	}
}

// buildDeployPluginsNoMock compiles the plugin binaries, packages them into a Docker image,
// loads the image into kind, and patches the argo-rollouts controller without K6_BASE_URL
// so the plugins call the real Grafana Cloud k6 API.
func buildDeployPluginsNoMock(clusterName string) env.Func {
	return func(ctx context.Context, cfg *envconf.Config) (context.Context, error) {
		goarch := runtime.GOARCH
		log.Printf("Building binaries for linux/%s (live mode)...", goarch)

		moduleRoot, err := filepath.Abs("..")
		if err != nil {
			return ctx, fmt.Errorf("resolve module root: %w", err)
		}

		gopath := os.Getenv("GOPATH")
		if gopath == "" || !filepath.IsAbs(gopath) {
			home, _ := os.UserHomeDir()
			gopath = filepath.Join(home, "go")
		}
		baseEnv := make([]string, 0, len(os.Environ()))
		for _, e := range os.Environ() {
			if !strings.HasPrefix(e, "GOPATH=") {
				baseEnv = append(baseEnv, e)
			}
		}

		tmpDir, err := os.MkdirTemp("", "e2e-plugins")
		if err != nil {
			return ctx, fmt.Errorf("create temp dir: %w", err)
		}
		defer os.RemoveAll(tmpDir)

		for _, binary := range []struct {
			name string
			path string
		}{
			{"metric-plugin", "./cmd/metric-plugin"},
			{"step-plugin", "./cmd/step-plugin"},
		} {
			outPath := filepath.Join(tmpDir, binary.name)
			cmd := exec.Command("go", "build", "-o", outPath, binary.path)
			cmd.Dir = moduleRoot
			cmd.Env = append(baseEnv, "GOOS=linux", "GOARCH="+goarch, "CGO_ENABLED=0", "GOPATH="+gopath)
			if out, err := cmd.CombinedOutput(); err != nil {
				return ctx, fmt.Errorf("build %s: %w\n%s", binary.name, err, string(out))
			}
			log.Printf("Built %s", binary.name)
		}

		dockerfile := "FROM alpine:3.19\n" +
			"COPY metric-plugin step-plugin /plugins/\n" +
			"RUN chmod 755 /plugins/metric-plugin /plugins/step-plugin\n" +
			"USER 65534\n"
		if err := os.WriteFile(filepath.Join(tmpDir, "Dockerfile"), []byte(dockerfile), 0o644); err != nil {
			return ctx, fmt.Errorf("write Dockerfile: %w", err)
		}

		const imageName = "k6-plugin-binaries:e2e"
		if out, err := exec.Command("docker", "build", "-t", imageName, tmpDir).CombinedOutput(); err != nil {
			return ctx, fmt.Errorf("docker build: %w\n%s", err, string(out))
		}
		log.Printf("Built Docker image %s", imageName)

		if out, err := exec.Command("kind", "load", "docker-image", imageName, "--name", clusterName).CombinedOutput(); err != nil {
			return ctx, fmt.Errorf("kind load docker-image: %w\n%s", err, string(out))
		}
		log.Printf("Loaded %s into kind cluster %s", imageName, clusterName)

		configPath := filepath.Join(moduleRoot, "e2e", "testdata", "argo-rollouts-config.yaml")
		if err := runKubectl(cfg, "apply", "-f", configPath); err != nil {
			return ctx, fmt.Errorf("apply argo-rollouts-config: %w", err)
		}

		if err := patchArgoRolloutsController(cfg, ""); err != nil {
			return ctx, err
		}

		log.Println("Plugin binaries loaded (live mode, no mock), controller ready")
		return ctx, nil
	}
}

// patchArgoRolloutsController applies a strategic merge patch to the argo-rollouts deployment:
// injects plugin binaries via initContainer into an emptyDir volume mounted at /tmp/argo-rollouts.
// When k6BaseURL is non-empty, sets K6_BASE_URL in the controller env (mock mode).
// When k6BaseURL is empty, no K6_BASE_URL is set and plugins use the real k6 API.
func patchArgoRolloutsController(cfg *envconf.Config, k6BaseURL string) error {
	envJSON := "[]"
	if k6BaseURL != "" {
		envJSON = fmt.Sprintf(`[{"name": "K6_BASE_URL", "value": %q}]`, k6BaseURL)
	}

	deployPatch := fmt.Sprintf(`{
		"spec": {
			"template": {
				"spec": {
					"initContainers": [{
						"name": "copy-plugins",
						"image": "k6-plugin-binaries:e2e",
						"imagePullPolicy": "Never",
						"securityContext": {"runAsUser": 65534},
						"command": ["sh", "-c", "cp /plugins/metric-plugin /out/metric-plugin && cp /plugins/step-plugin /out/step-plugin && chmod +x /out/metric-plugin /out/step-plugin"],
						"volumeMounts": [{"name": "plugin-dir", "mountPath": "/out"}]
					}],
					"containers": [{
						"name": "argo-rollouts",
						"env": %s,
						"volumeMounts": [{"name": "plugin-dir", "mountPath": "/tmp/argo-rollouts"}]
					}],
					"volumes": [{"name": "plugin-dir", "emptyDir": {}}]
				}
			}
		}
	}`, envJSON)

	if err := runKubectl(cfg, "patch", "deployment/argo-rollouts",
		"-n", "argo-rollouts", "--type=strategic", "-p", deployPatch); err != nil {
		return fmt.Errorf("patch argo-rollouts deployment: %w", err)
	}

	if err := runKubectl(cfg, "rollout", "status", "deployment/argo-rollouts",
		"-n", "argo-rollouts", "--timeout=120s"); err != nil {
		_ = runKubectl(cfg, "get", "pods", "-n", "argo-rollouts", "-o", "wide")
		_ = runKubectl(cfg, "describe", "pods", "-n", "argo-rollouts")
		return fmt.Errorf("wait for argo-rollouts after patch: %w", err)
	}
	return nil
}

// runKubectl executes a kubectl command using the kubeconfig from envconf.
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

// kubectlApplyStdin applies YAML from a string via kubectl stdin.
func kubectlApplyStdin(cfg *envconf.Config, yamlContent string) error {
	cmd := exec.Command("kubectl", "--kubeconfig", cfg.KubeconfigFile(), "apply", "-f", "-")
	cmd.Stdin = strings.NewReader(yamlContent)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %w", string(out), err)
	}
	return nil
}
