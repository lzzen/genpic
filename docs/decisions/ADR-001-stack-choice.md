# ADR-001 — Backend stack and module boundaries

**Date**: 2026-05-08  
**Status**: Accepted  
**Author**: @pyq

## Context

The platform needs a backend that:

1. Can be deployed as a compiled binary (source-code confidentiality requirement).
2. Has idiomatic support for high-concurrency HTTP and long upstream calls.
3. Has a gentle learning curve for a team coming primarily from PHP/Hyperf.
4. Supports a single-binary deployment for MVP Lite without a build chain.

Two candidates were evaluated: **Hyperf (PHP/Swoole)** and **Go (standard library + thin router)**.

## Decision

**Go** is chosen as the primary backend language. The HTTP layer uses the
**standard `net/http`** library for MVP Lite, plus **`gopkg.in/yaml.v3`** only to
read `config.yaml` (`mvp_lite` defaults). For the Full Platform (`cmd/genpic`), **`chi`** is added as the sole HTTP router
dependency: it is interface-compatible with `net/http`, tiny (~1 500 lines),
and the team can swap it out without rewriting handler logic.

### Module boundaries (single-repo, can split later)

```
genpic/                  ← Go module root; also embeds web/ assets
  cmd/
    mvplite/             ← MVP Lite binary (net/http + yaml for config.yaml)
    genpic/              ← Full platform binary (chi + future deps)
  internal/              ← Not importable by external modules
    api/                 ← HTTP handlers, DTOs, middleware wiring
    auth/                ← Platform API-key validation, session
    billing/             ← Credit pre-deduction, actual deduction, reversal
    provider/            ← Thin adapter glue (calls pkg/provider registry)
      openai/
      gemini/
      wan/
    storage/             ← Upload, signed URL, lifecycle
  pkg/                   ← Reusable, independently testable
    httpclient/
    provider/            ← Provider interface + registry + fake
    objstore/
    idempotency/
    ratelimit/
    billing/
    auth/
    errors/
    logger/
  contracts/             ← providers.yaml (machine-readable model contract table)
  docs/
    decisions/           ← ADRs (this directory)
    architecture.md
    runbook.md
  web/                   ← Static frontend assets (embedded by static_embed.go)
  openapi.yaml           ← OpenAPI 3.1 contract (source of truth for v1 routes)
```

### Dependency rules

- `cmd/*` may import `internal/*` and `pkg/*`.
- `internal/*` may import `pkg/*` but never another `internal` package outside its own subtree.
- `pkg/*` must not import `internal/*` or `cmd/*`.
- Three-party libraries are wrapped in a `pkg/` thin shell before use in
  business code. Swapping a library only touches the wrapper.

### CI cross-compilation

`GOOS=linux GOARCH=amd64 go build ./...` runs in CI on every PR. A secondary
target (`GOOS=linux GOARCH=arm64`) is added to catch architecture-specific
issues before ARM deployments.

## Options considered

| Option | Pros | Cons |
|---|---|---|
| `net/http` stdlib only | Zero deps, easiest auditing | Verbose routing for large route tables |
| `chi` | Idiomatic, tiny, stdlib-compatible | One external dependency |
| `echo` / `fiber` | More features (DI, built-in validation) | Larger surface, harder to audit |

The extra features of `echo`/`fiber` are not needed at M0; `chi` is the
sweet spot for auditability and ergonomics.

## Consequences

- All HTTP handlers are `http.Handler`-compatible; `chi` is transparent in
  unit tests (pass a `net/http/httptest.ResponseRecorder` directly).
- MVP Lite (`cmd/mvplite`) keeps a single YAML dependency for `config.yaml` and is not
  upgraded to `chi`; it remains a small `net/http` reference implementation.
- If the team later needs gRPC or WebSocket, those are added as separate
  `cmd/` entry points, not bolted onto the existing HTTP mux.
