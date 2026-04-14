# Phase 5: CI Pipeline Fix - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-04-15
**Phase:** 05-ci-pipeline-fix
**Areas discussed:** Kind install method, Workflow invocation

---

## Kind Install Method

| Option | Description | Selected |
|--------|-------------|----------|
| go install (Recommended) | go install sigs.k8s.io/kind@v0.27.0 — Go project already has setup-go, no extra action, pinned version | ✓ |
| helm/kind-action@v1 | GitHub Action that installs kind + creates cluster — but e2e-framework already manages cluster lifecycle | |
| curl binary | Download pre-built binary from kind releases — faster than go install but another URL to maintain | |

**User's choice:** go install (Recommended)
**Notes:** Consistent with Go toolchain already configured in workflow

---

## Workflow Invocation

| Option | Description | Selected |
|--------|-------------|----------|
| make test-e2e (Recommended) | DRY — Makefile already has the right flags. CI and local dev stay in sync automatically. | ✓ |
| Inline go test | Explicit in workflow file — but duplicates Makefile flags and can drift | |

**User's choice:** make test-e2e (Recommended)
**Notes:** Avoids flag duplication between Makefile and workflow file

---

## Claude's Discretion

- Kind version pin (use latest stable at implementation time)
- Whether to consolidate build step to also use Makefile target

## Deferred Ideas

None
