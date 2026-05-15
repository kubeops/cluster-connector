# AGENTS.md

This file provides guidance to coding agents (e.g. Claude Code, claude.ai/code) when working with code in this repository.

## Repository purpose

Go module `kubeops.dev/cluster-connector` — proxies Kubernetes API traffic between a central hub and one or more remote clusters **over NATS**. The hub-side `proxy-server` advertises a custom `http.RoundTripper` that wraps a request, publishes it on a per-cluster NATS subject, waits for the agent in the target cluster to reply, and returns the response as if it had come straight from the cluster's API. Lets the hub reach managed clusters that don't expose their kube API publicly — they just need outbound NATS connectivity.

Three binaries:
- `cluster-connector` — the main binary (run as the in-cluster agent or as the hub proxy, depending on flags).
- `proxy-server` — standalone hub-side proxy server.
- `demo-client` / `demo-server` — runnable demos for development.

## Architecture

- `cmd/cluster-connector/`, `cmd/proxy-server/`, `cmd/demo-client/`, `cmd/demo-server/` — four entry points.
- `pkg/cmds/`:
  - `root.go` — Cobra root.
  - `run.go` — long-running connector command.
- `pkg/link/lib.go` — connector "link" library that wires the agent to the hub.
- `pkg/transport/`:
  - `transport.go` — the custom `http.RoundTripper` (`New(...)`) built on top of NATS. Refuses custom transports / proxies so behavior is predictable.
  - `nats.go` — NATS client wrapper.
  - `cache.go`, `cache_test.go` — TLS config cache (so repeated calls to the same cluster reuse the same round-tripper instance).
  - `types.go`, `transport_test.go`.
- `pkg/shared/` — types shared across hub and agent:
  - `connector.go` — connector wiring; references `go.bytebuilders.dev/license-verifier/info` (the binary is license-gated).
  - `license.go` — license verification.
  - `types.go` — `SubjectNames` (NATS subjects) and friends.
- `pkg/clientcmd/`, `pkg/rest/`, `pkg/http/` — kubeconfig + HTTP plumbing.
- `Dockerfile.in` (PROD, distroless), `Dockerfile.dbg` (debian) — two image variants (no UBI).
- `hack/`, `Makefile` — AppsCode build harness.
- `docs/reference/` — generated CLI reference.
- `vendor/` — checked-in deps.

NATS is the load-bearing transport. Don't replace it lightly — the design depends on the agent dialing out to a NATS broker rather than the hub dialing the agent.

## Common commands

All Make targets run inside `ghcr.io/appscode/golang-dev` — Docker must be running.

- `make ci` — CI pipeline.
- `make build` / `make all-build` — build host or all-platform binaries.
- `make fmt`, `make lint`, `make unit-tests` / `make test` — standard.
- `make verify` — `verify-gen verify-modules`; `go mod tidy && go mod vendor` must leave the tree clean.
- `make container` — build PROD and DBG images.
- `make push` — push both; `make docker-manifest` writes multi-arch manifests; `make release` is the full publish flow.
- `make push-to-kind` / `make deploy-to-kind` — load into Kind and Helm-install.
- `make add-license` / `make check-license` — manage license headers.

Run a single Go test (requires a local Go toolchain):

```
go test ./pkg/transport/... -run TestName -v
```

## Conventions

- Module path is `kubeops.dev/cluster-connector` (vanity URL). Imports must use that.
- License: **AppsCode Community License 1.0.0** for newer files (`pkg/shared/`), older bits still carry Apache-2.0. Use the package's existing header style when adding files.
- Sign off commits (`git commit -s`); contributions follow the DCO.
- Vendor directory is checked in — `go mod tidy && go mod vendor` must leave the tree clean.
- Binary is license-gated via `go.bytebuilders.dev/license-verifier`; affects e2e/runtime, not unit tests.
- The custom `http.RoundTripper` in `pkg/transport/` deliberately refuses non-default `transport.Config.Transport` / `Config.Proxy` — those checks are a security boundary, do not relax them.
- Two Dockerfiles, one binary — keep `Dockerfile.in` and `Dockerfile.dbg` in sync.
- Four binaries (`cluster-connector`, `proxy-server`, `demo-client`, `demo-server`) — only the first two are production; demos are runtime fixtures.
