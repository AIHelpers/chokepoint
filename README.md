# Chokepoint — LLM Call Governance & Observability Gateway

Chokepoint is a drop-in reverse proxy that sits between your application and
your LLM providers (OpenAI, Anthropic, ...). Every call flows through
one interception point where Chokepoint can detect and redact sensitive
data, enforce policy (block/redact/route), track cost and latency, and
write an immutable audit log — the API-gateway pattern the industry
already uses for microservices, applied to LLM calls.

## Why

Teams shipping multiple AI features lose visibility fast: which
feature is calling which model, what it's costing, whether a prompt
just leaked a customer's SSN to a third-party API, and whether that's
even allowed under company policy. Chokepoint centralizes that.

## Features

- **Drop-in proxy** — point your OpenAI/Anthropic SDK base URL at
  Chokepoint; no code rewrite required.
- **PII / secret detection** — scans prompts for email, phone, SSN,
  credit card, IP address, AWS keys, API keys, and JWTs before they
  leave your org.
- **Policy engine** — ordered rules that block, redact, or reroute a
  call based on detected PII, model, team, or prompt size. Ships with
  a sensible default rule set; fully replaceable at runtime via the
  API.
- **Cost & usage tracking** — per-call cost computed from token usage
  against a configurable price list, aggregated by model, team, and
  feature.
- **Audit log** — immutable, queryable call history with redacted
  excerpts by default; raw content retention is an explicit opt-in.
- **Configurable retention** — a background pruner removes records
  past the retention window.
- **Latency & error monitoring** — per provider/model latency,
  timeout, and rate-limit tracking, exposed as JSON and Prometheus
  text format.
- **Dashboard** — a zero-dependency HTML dashboard served at `/`.

## Quick start

```bash
make build
./bin/chokepoint
# -> listening on :8080
```

Point your app at Chokepoint instead of the provider directly:

```bash
# OpenAI SDK
export OPENAI_BASE_URL="http://localhost:8080/proxy/openai"

# Anthropic SDK
export ANTHROPIC_BASE_URL="http://localhost:8080/proxy/anthropic"
```

Attach team/feature attribution with two optional headers:

```
X-Chokepoint-Team: search
X-Chokepoint-Feature: autocomplete
```

Open `http://localhost:8080/` for the dashboard, or query the API
directly:

```bash
curl localhost:8080/api/stats
curl localhost:8080/api/logs?limit=20
curl localhost:8080/api/metrics/latency
curl localhost:8080/metrics          # Prometheus format
```

## Policy engine

Rules are evaluated in order; the first match wins. Update them live:

```bash
curl -X PUT localhost:8080/api/policies \
  -H 'Content-Type: application/json' \
  -d '[{"name":"block-legal-team","condition":{"TeamEquals":"legal"},"action":"block"}]'
```

Default rules (see `internal/policy/engine.go`):

| Rule | Condition | Action |
|---|---|---|
| `block-secrets` | API key / AWS key / JWT detected | block |
| `redact-pii` | email / phone / SSN / credit card detected | redact |
| `route-long-context-to-cheaper-model` | prompt > 50k tokens (estimated) | route to `gpt-4o-mini` |

## Configuration

All settings are environment variables (see `internal/config/config.go`):

| Variable | Default | Description |
|---|---|---|
| `CHOKEPOINT_LISTEN_ADDR` | `:8080` | HTTP listen address |
| `CHOKEPOINT_UPSTREAM_TIMEOUT` | `30s` | Timeout for upstream provider calls |
| `CHOKEPOINT_RETENTION_DAYS` | `90` | Audit log retention window (0 disables pruning) |
| `CHOKEPOINT_RETAIN_RAW_CONTENT` | `false` | Store raw prompt/response text instead of redacted excerpts |
| `CHOKEPOINT_MAX_EXCERPT_CHARS` | `200` | Max length of stored prompt/response excerpts |
| `CHOKEPOINT_OPENAI_BASE_URL` | `https://api.openai.com` | OpenAI upstream override (for testing) |
| `CHOKEPOINT_ANTHROPIC_BASE_URL` | `https://api.anthropic.com` | Anthropic upstream override |

## Architecture

```
cmd/chokepoint/          entrypoint: wiring + graceful shutdown
internal/
  proxy/             the gateway http.Handler — the single interception point
  provider/          vendor-agnostic request/response parsing (OpenAI, Anthropic)
  pii/               PII & secret detection + redaction
  policy/            rule engine (block / redact / route)
  cost/              per-model cost calculation + spend anomaly detection
  metrics/           latency/error tracking, Prometheus exposition
  store/             audit log interface + in-memory implementation
  audit/             background retention pruner
  api/               dashboard REST API + embedded HTML dashboard
  config/            environment-based configuration
```

The audit log is defined by the `store.Store` interface — the shipped
`MemoryStore` is suitable for development and small deployments;
production deployments should implement `Store` against
ClickHouse/Postgres and swap it in at startup in `cmd/chokepoint/main.go`.

## Development

```bash
make test        # run unit tests
make test-race   # run with the race detector
make cover        # test coverage report
make vet          # go vet
make fmt-check    # gofmt check
make lint         # fmt-check + vet (+ golangci-lint if installed)
```

All packages currently have 80–100% test coverage. `golangci-lint`
could not be installed in the environment this project was built in
(its transitive dependencies reach hosts outside the sandbox's network
allowlist), so linting here relies on `go vet` and `gofmt`; a
`.golangci.yml` is included for environments with full network access.

## Security notes

- Prompt/response content is redacted before being written to the
  audit log by default (`CHOKEPOINT_RETAIN_RAW_CONTENT=false`). Enabling
  raw content retention is a compliance-relevant decision — make it
  deliberately.
- The PII detector is regex-based and favors precision over recall.
  It is not a substitute for a full DLP solution in high-stakes
  environments; pair it with an NER-based classifier for nuanced
  natural-language PII if your compliance requirements demand it.
- `X-Chokepoint-Team` / `X-Chokepoint-Feature` are trusted request headers in
  this MVP. Production deployments should authenticate callers (e.g.
  API keys mapped to a team) rather than trusting client-supplied
  attribution.
