# Phase 7: Foundation & Kubernetes Client - Research

**Researched:** 2026-04-15
**Domain:** Go provider routing, Kubernetes client-go, ConfigMap operations
**Confidence:** HIGH

## Summary

Phase 7 introduces provider routing, a Kubernetes client, and ConfigMap script sourcing to the existing plugin architecture. The codebase already has a clean `provider.Provider` interface, a working `GrafanaCloudProvider` implementation, and established patterns (functional options, stateless config, mock provider) that the new Router and K6OperatorProvider will follow directly.

The key technical components are: (1) a `provider.Router` that implements `provider.Provider` and dispatches to the correct backend based on a `provider` field in `PluginConfig`, (2) a `K6OperatorProvider` struct that lazily initializes a `kubernetes.Interface` via `sync.Once` + `rest.InClusterConfig()`, and (3) ConfigMap reading via the typed `CoreV1().ConfigMaps(ns).Get()` API. All three Kubernetes packages (`k8s.io/client-go`, `k8s.io/api`, `k8s.io/apimachinery`) are already in `go.sum` as indirect dependencies at v0.34.1 -- they just need promotion to direct in `go.mod`.

**Primary recommendation:** Build the Router as a thin multiplexer implementing `provider.Provider`, create a stub `K6OperatorProvider` with lazy k8s client init and ConfigMap reading, and wire everything through `main.go` via functional options.

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions
- **D-01:** Router implements Provider pattern -- a thin `provider.Router` multiplexer that implements the `provider.Provider` interface and delegates to the correct backend based on the `provider` field in PluginConfig. metric/step plugins receive the Router and remain provider-agnostic.
- **D-02:** Routing is per-call, not per-binary. The `provider` field in each AnalysisTemplate/Rollout config selects the backend. Empty or `"grafana-cloud"` defaults to existing Grafana Cloud provider. `"k6-operator"` routes to the k6-operator provider.
- **D-03:** main.go creates the Router via `provider.NewRouter()` with `WithProvider()` functional options. Both providers registered at startup; k6-operator uses lazy client init so grafana-cloud-only deployments never touch the k8s API.
- **D-04:** Single PluginConfig struct with optional fields. All provider-specific fields added with `omitempty`. Provider-specific fields grouped by comments. New fields for Phase 7: `Provider string`, `ConfigMapRef *ConfigMapRef`, `Namespace string`.
- **D-05:** Validation is per-provider. Each provider implements a `Validate(cfg *PluginConfig) error` method. Shared validation (metric field required, timeout parsing) stays on `PluginConfig.Validate()`. The Router calls provider-specific validation before dispatching.
- **D-06:** K6OperatorProvider owns its Kubernetes client as a struct field. Lazy initialization via `sync.Once` in an `ensureClient()` method that calls `rest.InClusterConfig()` + `kubernetes.NewForConfig()`. Provider is self-contained.
- **D-07:** Testing uses functional option `WithClient(fake)` to inject a fake client, bypassing lazy InClusterConfig. Matches the existing `WithBaseURL()` pattern on GrafanaCloudProvider.
- **D-08:** ConfigMap reading lives inside K6OperatorProvider, not a separate package. The provider reads the ConfigMap as part of its flow.
- **D-09:** ConfigMapRef is a simple struct with `Name` and `Key` fields. Namespace defaults to the rollout's namespace. Mirrors how Kubernetes volumes reference ConfigMaps.
- **D-10:** Phase 7 validates ConfigMap existence + key presence + non-empty content only. No script content parsing or k6-specific validation.

### Claude's Discretion
- Package layout for the k6-operator provider (e.g., `internal/provider/operator/`)
- ConfigMapRef struct location (config.go or separate file)
- Error message wording and slog field names

### Deferred Ideas (OUT OF SCOPE)
None
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| FOUND-01 | Plugin config accepts a `provider` field that routes to the correct execution backend (grafana-cloud default for backward compat, k6-operator) | Router pattern (D-01/D-02), PluginConfig extension (D-04), per-provider validation (D-05) |
| FOUND-02 | Plugin initializes a Kubernetes client via `rest.InClusterConfig()` in InitPlugin, promoted from indirect to direct dependency | Lazy init via sync.Once (D-06), client-go v0.34.1 already in go.sum, WithClient(fake) for testing (D-07) |
| FOUND-03 | Plugin reads k6 .js script content from a Kubernetes ConfigMap referenced by name and key in plugin config | ConfigMapRef struct (D-09), CoreV1().ConfigMaps().Get() API, validation of existence + key + non-empty (D-10) |
</phase_requirements>

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Provider routing | Plugin binary (Go) | -- | Pure in-process dispatch based on config field; no network involved |
| K8s client initialization | Plugin binary (Go) | Kubernetes API server | Plugin calls `rest.InClusterConfig()` which reads service account token from pod filesystem, then connects to API server |
| ConfigMap reading | Plugin binary (Go) | Kubernetes API server | Plugin uses typed client to GET a ConfigMap resource from the API server |
| Config validation | Plugin binary (Go) | -- | JSON unmarshalling and field validation are pure logic |

## Standard Stack

### Core (additions for Phase 7)
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `k8s.io/client-go` | v0.34.1 | Kubernetes typed client, in-cluster config, fake clientset | Already in go.sum as indirect. Standard K8s client library. [VERIFIED: go.sum] |
| `k8s.io/api` | v0.34.1 | Core API types (ConfigMap, etc.) | Already in go.sum as indirect. Required by client-go. [VERIFIED: go.sum] |
| `k8s.io/apimachinery` | v0.34.1 | Meta types (metav1.GetOptions, etc.) | Already in go.mod as direct dep (used by metric plugin). [VERIFIED: go.mod] |

### Supporting (no changes)
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `sync` (stdlib) | Go 1.26.2 | `sync.Once` for lazy client initialization | K6OperatorProvider.ensureClient() |
| `k8s.io/client-go/kubernetes/fake` | v0.34.1 | Fake Kubernetes clientset for unit tests | All K6OperatorProvider tests |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| `sync.Once` | `sync.OnceValues[kubernetes.Interface, error]` | OnceValues (Go 1.21+) is cleaner for returning (client, error) pair, but D-06 explicitly calls for sync.Once + ensureClient() pattern which is more conventional in K8s codebases |
| Typed client-go | Dynamic client (unstructured) | STATE.md mentions dynamic client for TestRun CRD in Phase 8, but Phase 7 only reads ConfigMaps which are core types -- typed client is correct here |

**Dependency promotion:**
```bash
# Promote indirect -> direct in go.mod
go get k8s.io/client-go@v0.34.1 k8s.io/api@v0.34.1
```

**Version verification:**
- `k8s.io/client-go` v0.34.1: already in go.sum [VERIFIED: go.sum]
- `k8s.io/api` v0.34.1: already in go.sum [VERIFIED: go.sum]
- `k8s.io/apimachinery` v0.34.1: already in go.mod [VERIFIED: go.mod]
- Go version: 1.26.2 installed, go.mod targets 1.24.9 [VERIFIED: `go version`]

## Architecture Patterns

### System Architecture Diagram

```
AnalysisTemplate / Rollout YAML
        |
        | JSON config (provider: "grafana-cloud" | "k6-operator")
        v
  K6MetricProvider / K6StepPlugin
        |
        | provider.Provider interface
        v
  provider.Router  <-- implements provider.Provider
        |
        |-- provider == "" || "grafana-cloud"
        |       |
        |       v
        |   cloud.GrafanaCloudProvider  (existing, unchanged)
        |       |
        |       v
        |   Grafana Cloud k6 REST API
        |
        |-- provider == "k6-operator"
                |
                v
            operator.K6OperatorProvider  (new, Phase 7 stub)
                |
                |-- ensureClient() [sync.Once]
                |       |
                |       v
                |   rest.InClusterConfig() -> kubernetes.NewForConfig()
                |
                |-- readScript(ctx, cfg)
                |       |
                |       v
                |   CoreV1().ConfigMaps(ns).Get(name) -> data[key]
                |
                |-- TriggerRun / GetRunResult / StopRun
                        |
                        v
                    Phase 8: TestRun CRD creation (stub returns error in Phase 7)
```

### Recommended Project Structure
```
internal/
  provider/
    provider.go          # Provider interface (unchanged)
    config.go            # PluginConfig + ConfigMapRef (extended)
    router.go            # NEW: Router multiplexer
    router_test.go       # NEW: Router tests
    cloud/
      cloud.go           # GrafanaCloudProvider (unchanged)
      cloud_test.go      # (unchanged)
      metrics.go         # (unchanged)
      types.go           # (unchanged)
    operator/
      operator.go        # NEW: K6OperatorProvider stub
      operator_test.go   # NEW: K6OperatorProvider tests
    providertest/
      mock.go            # MockProvider (unchanged, reused by Router tests)
cmd/
  metric-plugin/
    main.go              # MODIFIED: wire Router instead of GrafanaCloudProvider
  step-plugin/
    main.go              # MODIFIED: wire Router instead of GrafanaCloudProvider
```

### Pattern 1: Router as Provider Facade
**What:** Router implements `provider.Provider` and dispatches to registered backends based on `cfg.Provider` field.
**When to use:** When multiple execution backends share a common interface.
**Example:**
```go
// Source: Pattern derived from existing provider.Provider interface
// and GrafanaCloudProvider constructor pattern

package provider

// Router multiplexes provider calls to the correct backend.
type Router struct {
    providers map[string]Provider
    fallback  string // default provider name
}

type RouterOption func(*Router)

func WithProvider(name string, p Provider) RouterOption {
    return func(r *Router) {
        r.providers[name] = p
    }
}

func NewRouter(opts ...RouterOption) *Router {
    r := &Router{
        providers: make(map[string]Provider),
        fallback:  "grafana-cloud",
    }
    for _, opt := range opts {
        opt(r)
    }
    return r
}

func (r *Router) resolve(cfg *PluginConfig) (Provider, error) {
    name := cfg.Provider
    if name == "" {
        name = r.fallback
    }
    p, ok := r.providers[name]
    if !ok {
        return nil, fmt.Errorf("unknown provider %q", name)
    }
    return p, nil
}

func (r *Router) TriggerRun(ctx context.Context, cfg *PluginConfig) (string, error) {
    p, err := r.resolve(cfg)
    if err != nil {
        return "", err
    }
    return p.TriggerRun(ctx, cfg)
}
// ... GetRunResult, StopRun, Name follow same pattern
```
[VERIFIED: pattern derived from existing cloud.go and provider.go in codebase]

### Pattern 2: Lazy K8s Client with sync.Once
**What:** K6OperatorProvider initializes its Kubernetes client on first use, not at construction time.
**When to use:** When the client is only needed for one provider and construction should not fail for other providers.
**Example:**
```go
// Source: k8s.io/client-go API docs + Go stdlib sync.Once

package operator

import (
    "sync"
    "k8s.io/client-go/kubernetes"
    "k8s.io/client-go/rest"
)

type K6OperatorProvider struct {
    clientOnce sync.Once
    client     kubernetes.Interface
    clientErr  error
}

func (p *K6OperatorProvider) ensureClient() (kubernetes.Interface, error) {
    p.clientOnce.Do(func() {
        cfg, err := rest.InClusterConfig()
        if err != nil {
            p.clientErr = fmt.Errorf("in-cluster config: %w", err)
            return
        }
        p.client, p.clientErr = kubernetes.NewForConfig(cfg)
    })
    return p.client, p.clientErr
}
```
[VERIFIED: rest.InClusterConfig returns (*Config, error), kubernetes.NewForConfig returns (*Clientset, error) -- confirmed via `go doc`]

### Pattern 3: ConfigMap Script Reading
**What:** Read a k6 script from a Kubernetes ConfigMap by name and key.
**When to use:** When the k6-operator provider needs a script body to embed in the TestRun CR.
**Example:**
```go
// Source: k8s.io/client-go/kubernetes/typed/core/v1.ConfigMapInterface

func (p *K6OperatorProvider) readScript(ctx context.Context, cfg *PluginConfig) (string, error) {
    client, err := p.ensureClient()
    if err != nil {
        return "", err
    }
    ns := cfg.Namespace
    if ns == "" {
        ns = "default"
    }
    cm, err := client.CoreV1().ConfigMaps(ns).Get(ctx, cfg.ConfigMapRef.Name, metav1.GetOptions{})
    if err != nil {
        return "", fmt.Errorf("get configmap %s/%s: %w", ns, cfg.ConfigMapRef.Name, err)
    }
    script, ok := cm.Data[cfg.ConfigMapRef.Key]
    if !ok {
        return "", fmt.Errorf("key %q not found in configmap %s/%s", cfg.ConfigMapRef.Key, ns, cfg.ConfigMapRef.Name)
    }
    if script == "" {
        return "", fmt.Errorf("key %q in configmap %s/%s is empty", cfg.ConfigMapRef.Key, ns, cfg.ConfigMapRef.Name)
    }
    return script, nil
}
```
[VERIFIED: ConfigMapInterface.Get(ctx, name, metav1.GetOptions{}) returns (*corev1.ConfigMap, error), ConfigMap.Data is map[string]string -- confirmed via `go doc`]

### Pattern 4: Fake Client for Testing
**What:** Inject a fake Kubernetes clientset to bypass in-cluster config requirement in tests.
**When to use:** All K6OperatorProvider unit tests.
**Example:**
```go
// Source: k8s.io/client-go/kubernetes/fake

import (
    "k8s.io/client-go/kubernetes/fake"
    corev1 "k8s.io/api/core/v1"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type Option func(*K6OperatorProvider)

func WithClient(c kubernetes.Interface) Option {
    return func(p *K6OperatorProvider) {
        p.client = c
        // Mark clientOnce as done so ensureClient() returns this client
        p.clientOnce.Do(func() {})
    }
}

// In tests:
cm := &corev1.ConfigMap{
    ObjectMeta: metav1.ObjectMeta{
        Name:      "k6-scripts",
        Namespace: "default",
    },
    Data: map[string]string{
        "test.js": "import http from 'k6/http'; ...",
    },
}
fakeClient := fake.NewSimpleClientset(cm)
p := NewK6OperatorProvider(WithClient(fakeClient))
```
[VERIFIED: fake.NewSimpleClientset accepts ...runtime.Object, returns *Clientset which implements kubernetes.Interface -- confirmed via `go doc`]

**Note on WithClient + sync.Once interaction:** When `WithClient` sets `p.client` directly and then calls `p.clientOnce.Do(func(){})`, the `sync.Once` is consumed with a no-op, so future `ensureClient()` calls return the pre-set client without running InClusterConfig. This is a standard pattern for dependency injection with sync.Once.

### Anti-Patterns to Avoid
- **Initializing K8s client at startup for all deployments:** Grafana-cloud-only users would get InClusterConfig errors if the plugin runs outside a cluster (e.g., during development/testing). Lazy init (D-06) avoids this.
- **Provider field in the Provider interface itself:** The routing decision belongs to the Router, not individual providers. Providers should not know about other providers.
- **Putting ConfigMap reading in a shared utility package:** D-08 explicitly places it inside K6OperatorProvider. Over-abstracting adds complexity for a single consumer.
- **Using `kubernetes.NewForConfigOrDie()`:** The `OrDie` variant calls `os.Exit(1)` on error. In a plugin running as a child process, this would kill the plugin without informing the controller. Always return errors.
- **Importing `controller-runtime`:** STATE.md notes using dynamic client for TestRun CRDs. For Phase 7 we only need typed client-go for ConfigMaps -- no controller-runtime needed.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Kubernetes REST client | Custom HTTP client with service account token parsing | `k8s.io/client-go/rest.InClusterConfig()` + `kubernetes.NewForConfig()` | Handles token rotation, CA cert loading, API server discovery. The service account token path and CA bundle path are standardized but not trivial |
| ConfigMap fetching | Raw HTTP GET to `/api/v1/namespaces/{ns}/configmaps/{name}` | `client.CoreV1().ConfigMaps(ns).Get()` | Type-safe, handles auth, retry, content negotiation. Also needed for fake client in tests |
| Fake K8s client for tests | Custom mock struct implementing kubernetes.Interface (60+ methods) | `k8s.io/client-go/kubernetes/fake.NewSimpleClientset()` | Pre-built, tracks objects, supports Get/List/Create/Delete. Implementing the full interface manually would be enormous |
| Thread-safe lazy init | Custom mutex + flag pattern | `sync.Once` | sync.Once is the standard Go primitive, handles all edge cases (concurrent calls, panics) |

**Key insight:** The Kubernetes client-go library handles a surprising amount of complexity (token rotation, TLS, HTTP/2 multiplexing, API discovery) that would be extremely error-prone to replicate.

## Common Pitfalls

### Pitfall 1: sync.Once Swallows Initialization Errors
**What goes wrong:** `sync.Once.Do(f)` runs `f` exactly once, even if `f` sets an error. Subsequent calls return without running `f` again. If you don't capture the error, every future call silently uses a nil client.
**Why it happens:** Developers expect retry behavior on failure.
**How to avoid:** Store both client and error as struct fields inside the Once function. `ensureClient()` always returns `(p.client, p.clientErr)`. If clientErr is non-nil, every call fails fast with the same error.
**Warning signs:** `nil pointer dereference` on the client after InClusterConfig fails.

### Pitfall 2: ConfigMap 1 MiB Size Limit
**What goes wrong:** Kubernetes ConfigMaps have a hard 1 MiB total size limit (etcd value size). Large k6 scripts that include test data or many imports can exceed this.
**Why it happens:** Users embed test data or fixtures directly in the k6 script.
**How to avoid:** Phase 7 scope is read-only (D-10) -- just document the limit. The REQUIREMENTS.md already lists PersistentVolume sourcing as future scope (SCRIPT-01). For now, fail with a clear error if the ConfigMap can't be fetched.
**Warning signs:** `etcdserver: request is too large` errors on ConfigMap creation (upstream of the plugin).

### Pitfall 3: Namespace Defaulting
**What goes wrong:** `cfg.Namespace` may be empty. If you pass empty string to `CoreV1().ConfigMaps("")`, client-go defaults to the `"default"` namespace, which may not be where the ConfigMap lives.
**Why it happens:** The rollout's namespace isn't directly available to the provider (it comes through config). Users forget to set it.
**How to avoid:** D-09 says "Namespace defaults to the rollout's namespace." In practice, this means the metric/step plugin must pass the namespace from the AnalysisRun/Rollout metadata into PluginConfig. For Phase 7, default to `"default"` if empty, and document that users should set `namespace` in their config.
**Warning signs:** "configmap not found" errors when the ConfigMap exists in a different namespace.

### Pitfall 4: Backward Compatibility in PluginConfig Deserialization
**What goes wrong:** Adding new fields to PluginConfig with `omitempty` works for JSON deserialization (unknown fields are ignored by default), but if you add required-field validation to the shared `Validate()` method, existing Grafana Cloud users who don't set the new fields will get validation errors.
**Why it happens:** Validation is tightened without considering the per-provider split.
**How to avoid:** D-05 specifies per-provider validation. The Router calls `provider.Validate(cfg)` on the resolved provider, not on the shared PluginConfig. Shared PluginConfig.Validate() only checks universal fields.
**Warning signs:** Existing AnalysisTemplates/Rollouts that worked in v0.2.0 suddenly fail after upgrade.

### Pitfall 5: Router.Name() Ambiguity
**What goes wrong:** The Router implements `provider.Provider`, which includes `Name() string`. But the Router is a multiplexer, not a single provider. Returning a fixed "router" name confuses logs.
**Why it happens:** Interface requires Name() but the Router doesn't have a single identity.
**How to avoid:** Router.Name() can return `"router"` for the interface contract, but all actual provider calls should log the resolved provider's name. The Router should log `"provider", resolvedProvider.Name()` in its dispatch methods.
**Warning signs:** Log lines showing `provider=router` with no indication of which backend handled the call.

### Pitfall 6: stdout Contamination from client-go
**What goes wrong:** `k8s.io/client-go` uses `klog` for logging, which by default writes to stderr (OK) but can be configured to write to stdout (breaks go-plugin handshake).
**Why it happens:** klog initialization is global and can be influenced by environment variables or init() functions.
**How to avoid:** The existing `setupLogging()` in main.go already configures slog to stderr. klog defaults to stderr. Don't import any package that calls `klog.InitFlags()` or redirects klog to stdout. The lint-stdout Makefile target catches this at build time.
**Warning signs:** `make lint-stdout` fails, or the plugin binary fails to start with handshake errors.

## Code Examples

### Extended PluginConfig with New Fields
```go
// Source: Existing config.go pattern + D-04 decisions

package provider

// ConfigMapRef references a key in a Kubernetes ConfigMap.
type ConfigMapRef struct {
    Name string `json:"name"`
    Key  string `json:"key"`
}

// PluginConfig holds configuration parsed from the AnalysisTemplate
// plugin config JSON. Passed to every provider method call (stateless pattern).
type PluginConfig struct {
    // Shared fields
    Timeout string `json:"timeout,omitempty"`
    Metric  string `json:"metric"`

    // Provider routing
    Provider string `json:"provider,omitempty"` // "grafana-cloud" (default) or "k6-operator"

    // Grafana Cloud fields
    TestRunID string `json:"testRunId,omitempty"`
    TestID    string `json:"testId,omitempty"`
    APIToken  string `json:"apiToken,omitempty"`
    StackID   string `json:"stackId,omitempty"`
    Aggregation string `json:"aggregation,omitempty"`

    // k6-operator fields
    ConfigMapRef *ConfigMapRef `json:"configMapRef,omitempty"`
    Namespace    string        `json:"namespace,omitempty"`
}
```
[VERIFIED: pattern matches existing config.go structure]

### main.go Wiring with Router
```go
// Source: Existing cmd/metric-plugin/main.go + D-03 decisions

// In cmd/metric-plugin/main.go and cmd/step-plugin/main.go:
cloudProvider := cloud.NewGrafanaCloudProvider(cloudOpts...)
operatorProvider := operator.NewK6OperatorProvider(operatorOpts...)

router := provider.NewRouter(
    provider.WithProvider("grafana-cloud", cloudProvider),
    provider.WithProvider("k6-operator", operatorProvider),
)

impl := metric.New(router) // or step.New(router) -- Router implements Provider
```
[VERIFIED: metric.New and step.New accept provider.Provider interface]

### K6OperatorProvider Stub (Phase 7 scope)
```go
// Source: Design from D-06, D-07, D-08 decisions

package operator

type K6OperatorProvider struct {
    clientOnce sync.Once
    client     kubernetes.Interface
    clientErr  error
}

// TriggerRun is a stub for Phase 7. Phase 8 will implement TestRun CRD creation.
func (p *K6OperatorProvider) TriggerRun(ctx context.Context, cfg *provider.PluginConfig) (string, error) {
    // Phase 7: validate config and read script to prove the pipeline works
    if cfg.ConfigMapRef == nil {
        return "", fmt.Errorf("configMapRef is required for k6-operator provider")
    }
    script, err := p.readScript(ctx, cfg)
    if err != nil {
        return "", err
    }
    slog.Info("k6 script loaded from configmap",
        "configmap", cfg.ConfigMapRef.Name,
        "key", cfg.ConfigMapRef.Key,
        "scriptLen", len(script),
    )
    // Phase 8 will create TestRun CR here using the script content
    return "", fmt.Errorf("k6-operator provider not yet implemented (Phase 8)")
}

func (p *K6OperatorProvider) Name() string { return "k6-operator" }
```
[VERIFIED: Provider interface requires TriggerRun, GetRunResult, StopRun, Name]

### Unit Test with Fake Client
```go
// Source: k8s.io/client-go/kubernetes/fake API

func TestReadScript_Success(t *testing.T) {
    cm := &corev1.ConfigMap{
        ObjectMeta: metav1.ObjectMeta{
            Name:      "k6-scripts",
            Namespace: "test-ns",
        },
        Data: map[string]string{
            "load-test.js": "import http from 'k6/http';\nexport default function() { http.get('http://test'); }",
        },
    }
    fakeClient := fake.NewSimpleClientset(cm)
    p := NewK6OperatorProvider(WithClient(fakeClient))
    cfg := &provider.PluginConfig{
        ConfigMapRef: &provider.ConfigMapRef{Name: "k6-scripts", Key: "load-test.js"},
        Namespace:    "test-ns",
    }
    script, err := p.readScript(context.Background(), cfg)
    require.NoError(t, err)
    assert.Contains(t, script, "k6/http")
}
```
[VERIFIED: fake.NewSimpleClientset accepts runtime.Object, corev1.ConfigMap implements runtime.Object]

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| `fake.NewSimpleClientset()` | `fake.NewClientset()` (preferred) | client-go v0.31+ | NewClientset supports field management. NewSimpleClientset still works and is not removed. Either works for Phase 7 -- use NewSimpleClientset for simplicity since we don't need apply configs |
| `sync.Once` + manual error field | `sync.OnceValues[T1, T2]` | Go 1.21 | OnceValues is a cleaner API but D-06 explicitly chose sync.Once pattern. Both are correct |

**Deprecated/outdated:**
- `k8s.io/client-go/kubernetes/fake.NewSimpleClientset`: Marked deprecated in favor of `NewClientset`, but still functional and widely used. For Phase 7's simple ConfigMap Get tests, either works. [VERIFIED: go doc output shows DEPRECATED note]

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | klog (used by client-go) defaults to stderr and won't corrupt go-plugin stdout handshake | Pitfall 6 | HIGH -- plugin would fail to start. Mitigated by existing lint-stdout CI check |
| A2 | ConfigMap namespace defaults to "default" when empty string is passed to client-go | Pitfall 3 | MEDIUM -- ConfigMap lookups would silently go to wrong namespace. Easy to test |
| A3 | `fake.NewSimpleClientset` is sufficient for Phase 7 tests (no apply configuration needed) | State of the Art | LOW -- can switch to NewClientset if needed |

## Open Questions (RESOLVED)

1. **Namespace sourcing for k6-operator provider**
   - What we know: D-09 says "Namespace defaults to the rollout's namespace." The metric plugin receives `*v1alpha1.AnalysisRun` (which has namespace in ObjectMeta) and the step plugin receives `*v1alpha1.Rollout` (also has namespace).
   - What's unclear: Should the namespace be extracted in the metric/step plugin layer and set on PluginConfig before calling the provider? Or should the provider receive the namespace separately?
   - Recommendation: The cleanest approach is to have the metric/step plugin extract the namespace from the AnalysisRun/Rollout ObjectMeta and populate `cfg.Namespace` if it's empty, before calling provider methods. This keeps the provider layer namespace-agnostic. However, current `parseConfig` functions don't receive the AnalysisRun/Rollout namespace. This needs a small change in Phase 7 or can be deferred to Phase 8 when the namespace is actually used for TestRun creation.
   - RESOLVED: Phase 7 uses cfg.Namespace with fallback to "default". Rollout namespace injection deferred to Phase 8 when namespace is consumed for TestRun CR creation.

2. **Per-provider validation method signature**
   - What we know: D-05 says each provider implements `Validate(cfg *PluginConfig) error`. Current code has no Validate method -- validation is inline in `parseConfig()`.
   - What's unclear: Should `Validate` be added to the `provider.Provider` interface, or should it be a separate interface that the Router checks for?
   - Recommendation: Add `Validate` to the Router's dispatch flow as a separate interface (`Validator`) that providers optionally implement. This avoids changing the Provider interface (which would require updating existing mock/tests). The Router checks `if v, ok := p.(Validator); ok { err := v.Validate(cfg) }`.
   - RESOLVED: Plan 01 uses IsGrafanaCloud() helper inline in parseConfig() to gate Grafana Cloud field validation. Plan 02 adds a Validate() method on K6OperatorProvider outside the Provider interface to avoid changing the interface contract.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go compiler | Build | Yes | 1.26.2 | -- |
| k8s.io/client-go | K8s client | Yes (indirect) | v0.34.1 | -- |
| k8s.io/api | ConfigMap type | Yes (indirect) | v0.34.1 | -- |
| golangci-lint | Linting | Yes | -- | -- |

**Missing dependencies with no fallback:** None
**Missing dependencies with fallback:** None

## Validation Architecture

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go testing + testify v1.11.1 |
| Config file | None (go test defaults) |
| Quick run command | `make test` |
| Full suite command | `make test` |

### Phase Requirements -> Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| FOUND-01 | Router dispatches to correct provider based on cfg.Provider field | unit | `go test -run TestRouter -v ./internal/provider/...` | Wave 0 |
| FOUND-01 | Empty/grafana-cloud defaults to GrafanaCloudProvider | unit | `go test -run TestRouter_Default -v ./internal/provider/...` | Wave 0 |
| FOUND-01 | Unknown provider returns error | unit | `go test -run TestRouter_Unknown -v ./internal/provider/...` | Wave 0 |
| FOUND-02 | K6OperatorProvider lazy-inits k8s client via WithClient | unit | `go test -run TestEnsureClient -v ./internal/provider/operator/...` | Wave 0 |
| FOUND-03 | readScript returns ConfigMap data by name+key | unit | `go test -run TestReadScript -v ./internal/provider/operator/...` | Wave 0 |
| FOUND-03 | readScript fails for missing ConfigMap | unit | `go test -run TestReadScript_NotFound -v ./internal/provider/operator/...` | Wave 0 |
| FOUND-03 | readScript fails for missing key | unit | `go test -run TestReadScript_MissingKey -v ./internal/provider/operator/...` | Wave 0 |
| FOUND-03 | readScript fails for empty content | unit | `go test -run TestReadScript_Empty -v ./internal/provider/operator/...` | Wave 0 |

### Sampling Rate
- **Per task commit:** `make test`
- **Per wave merge:** `make test && make lint`
- **Phase gate:** Full suite green before `/gsd-verify-work`

### Wave 0 Gaps
- [ ] `internal/provider/router_test.go` -- covers FOUND-01 (Router dispatch, defaults, unknown provider)
- [ ] `internal/provider/operator/operator_test.go` -- covers FOUND-02, FOUND-03 (client init, ConfigMap reading)

## Security Domain

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | No | K8s client uses in-cluster service account (automatic, no user auth) |
| V3 Session Management | No | No sessions -- stateless provider calls |
| V4 Access Control | Yes | RBAC: plugin service account needs `get` on `configmaps` in target namespace |
| V5 Input Validation | Yes | ConfigMapRef name/key validated before API call. Provider field validated against known set |
| V6 Cryptography | No | TLS handled by client-go automatically via InClusterConfig CA bundle |

### Known Threat Patterns for client-go + ConfigMap

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| ConfigMap read from wrong namespace | Information Disclosure | Validate namespace is set and matches rollout namespace |
| Overly broad RBAC (cluster-wide configmap access) | Elevation of Privilege | RBAC should scope to specific namespaces. Phase 10 (DOCS-01) will provide example ClusterRole |
| Service account token exposure | Information Disclosure | client-go handles token mounting and rotation automatically. No manual token handling |

## Sources

### Primary (HIGH confidence)
- Codebase files: `internal/provider/provider.go`, `config.go`, `cloud/cloud.go`, `cmd/*/main.go`, `go.mod` -- read directly
- `go doc k8s.io/client-go/rest.InClusterConfig` -- verified API signature
- `go doc k8s.io/client-go/kubernetes.NewForConfig` -- verified API signature
- `go doc k8s.io/client-go/kubernetes.Interface` -- verified interface definition
- `go doc k8s.io/client-go/kubernetes/typed/core/v1.ConfigMapInterface` -- verified Get() signature
- `go doc k8s.io/api/core/v1.ConfigMap` -- verified Data field is `map[string]string`
- `go doc k8s.io/client-go/kubernetes/fake` -- verified NewSimpleClientset and NewClientset
- `go doc sync.Once` -- verified Do(f func()) signature
- `go doc sync.OnceValues` -- verified availability in Go 1.21+
- `go.sum` -- verified k8s.io/client-go v0.34.1 and k8s.io/api v0.34.1 present

### Secondary (MEDIUM confidence)
- D-01 through D-10 decisions from 07-CONTEXT.md -- user-locked decisions guiding implementation

### Tertiary (LOW confidence)
- A1: klog stderr default assumption (likely correct but not verified with import trace)

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH -- all dependencies verified in go.sum/go.mod, API signatures confirmed via go doc
- Architecture: HIGH -- all patterns derived from existing codebase conventions and locked decisions
- Pitfalls: HIGH -- based on well-known client-go and sync.Once behaviors, verified via API docs

**Research date:** 2026-04-15
**Valid until:** 2026-05-15 (stable domain, Kubernetes client-go API doesn't change within minor versions)
