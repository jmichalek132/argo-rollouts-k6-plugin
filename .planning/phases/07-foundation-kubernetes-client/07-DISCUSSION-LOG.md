# Phase 7: Foundation & Kubernetes Client - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-04-15
**Phase:** 07-foundation-kubernetes-client
**Areas discussed:** Provider routing, Config structure, K8s client lifecycle, Script sourcing

---

## Provider Routing

| Option | Description | Selected |
|--------|-------------|----------|
| Factory in main.go | Each main.go reads provider field and constructs the right provider. Simple, explicit. | (initial) |
| Registry package | internal/provider/registry with Register()/Get() pattern. Self-registering providers via init(). | |
| InitPlugin routing | Move provider construction into InitPlugin() where config context is available. | |

**User's choice:** Factory in main.go (initial selection)

| Option | Description | Selected |
|--------|-------------|----------|
| InitPlugin reads configMap | Route once at startup from pluginConfig in controller ConfigMap. | (initial) |
| Per-measurement routing | Read provider from each AnalysisTemplate/Rollout config per call. | |

**User's choice:** InitPlugin reads configMap (initial selection)

**Follow-up:** Discovered InitPlugin() takes no arguments — no config is passed from controller. Re-evaluated approach.

| Option | Description | Selected |
|--------|-------------|----------|
| Env var at startup | K6_PROVIDER env var in ConfigMap plugin entry. Binary creates one provider. | (initial) |
| Per-call from config | Read provider field from each measurement/step config JSON. Lazy-init providers. | |

**User's choice:** Env var (initial selection)

**Follow-up:** User asked "is there a better way?" — presented Router implements Provider pattern as alternative.

| Option | Description | Selected |
|--------|-------------|----------|
| Router implements Provider | Thin multiplexer delegating based on config's provider field. metric/step plugins unchanged. | ✓ |
| Env var at startup | K6_PROVIDER env var. Simpler but less flexible. | |
| You decide | Let researcher/planner determine. | |

**User's choice:** Router implements Provider
**Notes:** Router is cleaner than env var — self-describing config, no operator coordination needed, both providers active in one deployment. Lazy k8s client init avoids penalizing grafana-cloud-only users.

---

## Config Structure

| Option | Description | Selected |
|--------|-------------|----------|
| Single struct, optional fields | All fields in one PluginConfig with omitempty. Flat JSON in AnalysisTemplates. | ✓ |
| Embedded provider configs | Base PluginConfig + nested provider-specific structs. Deeper JSON nesting. | |
| Interface + per-provider types | PluginConfig becomes interface. Two-phase unmarshal. Most type-safe but complex. | |

**User's choice:** Single struct, optional fields
**Notes:** Simple and flat. Provider-specific fields grouped by comments.

| Option | Description | Selected |
|--------|-------------|----------|
| Validate method per provider | Each provider implements Validate(cfg) error. Shared validation on PluginConfig.Validate(). | ✓ |
| Single parseConfig with switch | Keep validation in existing parseConfig functions with switch on provider field. | |

**User's choice:** Validate method per provider
**Notes:** Clean separation. Router calls provider-specific validation before dispatch.

---

## K8s Client Lifecycle

| Option | Description | Selected |
|--------|-------------|----------|
| K6OperatorProvider field | Provider owns kubernetes.Interface. Lazy init via sync.Once. Self-contained. | ✓ |
| Injected via constructor | main.go creates k8s client, passes to provider. Easier to test but main.go takes k8s dep. | |
| You decide | Let researcher/planner determine. | |

**User's choice:** K6OperatorProvider field
**Notes:** Provider is self-contained. ensureClient() with sync.Once on first call.

| Option | Description | Selected |
|--------|-------------|----------|
| Option func override | WithClient(fake) skips lazy InClusterConfig. Matches WithBaseURL pattern. | ✓ |
| Interface injection | Constructor requires kubernetes.Interface. No lazy init. | |

**User's choice:** Option func override
**Notes:** Consistent with existing GrafanaCloudProvider pattern.

---

## Script Sourcing

| Option | Description | Selected |
|--------|-------------|----------|
| Inside K6OperatorProvider | Provider reads ConfigMap in TriggerRun. Owns k8s client. Provider interface unchanged. | ✓ |
| Standalone ScriptSource service | Separate internal/script package. More extensible but extra layer. | |
| On the Provider interface | Add ReadScript() to Provider interface. Makes sourcing explicit in contract. | |

**User's choice:** Inside K6OperatorProvider
**Notes:** Provider is self-contained. Script reading is internal to the k6-operator flow.

| Option | Description | Selected |
|--------|-------------|----------|
| Name + key | Simple ConfigMapRef with name and key. Namespace defaults to rollout ns. | ✓ |
| Full ObjectReference | Kubernetes-style ObjectReference. Verbose for single resource type. | |

**User's choice:** Name + key
**Notes:** Mirrors Kubernetes volume ConfigMap references.

| Option | Description | Selected |
|--------|-------------|----------|
| Existence only | ConfigMap reachable, key exists, non-empty. Let k6 validate script. | ✓ |
| Existence + basic sniff | Also warn if missing 'export default function'. | |
| Existence + sniff + handleSummary | Also warn if missing handleSummary(). Couples Phase 7 to Phase 9. | |

**User's choice:** Existence only
**Notes:** Fail-fast on infrastructure issues. No script content parsing.

---

## Claude's Discretion

- Package layout for k6-operator provider
- ConfigMapRef struct location
- Error message wording and slog field names

## Deferred Ideas

None — discussion stayed within phase scope
