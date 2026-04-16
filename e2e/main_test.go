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
		installK6Operator(),
		preloadK6Image(kindClusterName),
		applyK6PluginRBAC(),
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

// installK6Operator installs the k6-operator CRDs and controller into the cluster.
// Waits for both CRD establishment and controller readiness.
func installK6Operator() env.Func {
	return func(ctx context.Context, cfg *envconf.Config) (context.Context, error) {
		log.Println("Installing k6-operator...")

		if err := runKubectl(cfg, "apply", "--server-side", "-f",
			"https://raw.githubusercontent.com/grafana/k6-operator/v1.3.2/bundle.yaml"); err != nil {
			return ctx, fmt.Errorf("install k6-operator: %w", err)
		}

		// Wait for CRDs to be established before any CR creation.
		// Addresses review concern: controller readiness alone is insufficient.
		if err := runKubectl(cfg, "wait", "--for=condition=Established",
			"crd/testruns.k6.io", "crd/privateloadzones.k6.io",
			"--timeout=60s"); err != nil {
			return ctx, fmt.Errorf("wait for k6-operator CRDs: %w", err)
		}

		if err := runKubectl(cfg, "rollout", "status", "deployment/k6-operator-controller-manager",
			"-n", "k6-operator-system", "--timeout=120s"); err != nil {
			return ctx, fmt.Errorf("wait for k6-operator controller: %w", err)
		}

		log.Println("k6-operator installed successfully")
		return ctx, nil
	}
}

// k6RunnerImage is the pinned k6 runner image used in e2e tests.
// Pinned for reproducibility -- grafana/k6:latest is non-deterministic.
const k6RunnerImage = "grafana/k6:0.56.0"

// preloadK6Image pulls the pinned k6 runner image and loads it into the kind cluster
// to avoid Docker Hub rate limits during k6-operator TestRun execution.
//
// Bypasses `kind load docker-image` / `kind load image-archive` because both invoke
// `ctr images import --all-platforms --digests` inside the kind node. On colima
// (macOS), colima's local layer cache only holds the host platform's blobs, so the
// other platforms' manifest digests cannot be resolved and the import aborts with
// `content digest <sha>: not found`. Instead: docker save → pipe tar via stdin to
// `ctr images import --digests` (no --all-platforms) directly inside each kind node.
// Works on Docker Desktop, colima, and standard CI runners.
func preloadK6Image(clusterName string) env.Func {
	return func(ctx context.Context, cfg *envconf.Config) (context.Context, error) {
		log.Printf("Pre-loading %s into kind...", k6RunnerImage)

		if out, err := exec.Command("docker", "pull", k6RunnerImage).CombinedOutput(); err != nil {
			log.Printf("Warning: failed to pull %s (may already be cached): %s", k6RunnerImage, string(out))
			// Non-fatal: image might already be present locally
		}

		tarPath := filepath.Join(os.TempDir(), "k6-runner-e2e.tar")
		defer os.Remove(tarPath)
		if out, err := exec.Command("docker", "save", "-o", tarPath, k6RunnerImage).CombinedOutput(); err != nil {
			return ctx, fmt.Errorf("docker save %s: %w\n%s", k6RunnerImage, err, string(out))
		}

		// Enumerate kind nodes so each one gets the image (single-node clusters = 1 iteration).
		nodesOut, err := exec.Command("kind", "get", "nodes", "--name", clusterName).CombinedOutput()
		if err != nil {
			return ctx, fmt.Errorf("kind get nodes: %w\n%s", err, string(nodesOut))
		}
		nodes := strings.Fields(string(nodesOut))
		if len(nodes) == 0 {
			return ctx, fmt.Errorf("no kind nodes found for cluster %q", clusterName)
		}

		tar, err := os.Open(tarPath)
		if err != nil {
			return ctx, fmt.Errorf("open tar: %w", err)
		}
		defer tar.Close()

		for _, node := range nodes {
			if _, err := tar.Seek(0, 0); err != nil {
				return ctx, fmt.Errorf("rewind tar: %w", err)
			}
			cmd := exec.Command("docker", "exec", "-i", node,
				"ctr", "--namespace=k8s.io", "images", "import",
				"--digests", "--snapshotter=overlayfs", "-")
			cmd.Stdin = tar
			if out, err := cmd.CombinedOutput(); err != nil {
				return ctx, fmt.Errorf("import into %s: %w\n%s", node, err, string(out))
			}
		}

		log.Printf("%s loaded into kind (%d node(s))", k6RunnerImage, len(nodes))
		return ctx, nil
	}
}

// applyK6PluginRBAC creates the ClusterRole and ClusterRoleBinding that grants the
// argo-rollouts controller ServiceAccount permissions to manage k6-operator CRDs,
// read pods/logs, and read ConfigMaps. Without this, k6-operator e2e tests hit 403.
func applyK6PluginRBAC() env.Func {
	return func(ctx context.Context, cfg *envconf.Config) (context.Context, error) {
		log.Println("Applying k6 plugin RBAC...")

		const rbacYAML = `apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: argo-rollouts-k6-plugin
rules:
  - apiGroups: ["k6.io"]
    resources: ["testruns", "privateloadzones"]
    verbs: ["create", "get", "list", "watch", "delete"]
  - apiGroups: [""]
    resources: ["pods"]
    verbs: ["list"]
  - apiGroups: [""]
    resources: ["pods/log"]
    verbs: ["get"]
  - apiGroups: [""]
    resources: ["configmaps"]
    verbs: ["get"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: argo-rollouts-k6-plugin
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: argo-rollouts-k6-plugin
subjects:
  - kind: ServiceAccount
    name: argo-rollouts
    namespace: argo-rollouts`

		if err := kubectlApplyStdin(cfg, rbacYAML); err != nil {
			return ctx, fmt.Errorf("apply k6 plugin RBAC: %w", err)
		}

		log.Println("k6 plugin RBAC applied")
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
