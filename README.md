# dcx — Agent-Native Data Cloud CLI

An agent-native CLI for Google Cloud's Data Cloud, built in Go.
One binary for BigQuery, Spanner, AlloyDB, Cloud SQL, and Looker —
with structured output, typed errors, and an MCP bridge for AI agents.

> **Status:** Go MVP functional — 79 commands across 11 domains.
> Benchmarked at **5x faster** than `bq` with token cost within 6%.
> See [docs/benchmark_results_bigquery.md](docs/benchmark_results_bigquery.md)
> for measured results.

## Why dcx

- **One CLI for five services.** BigQuery, Spanner, AlloyDB, Cloud SQL,
  and Looker through a single binary — no per-service tooling.
- **Machine-safe output.** Structured JSON on stdout, typed errors on
  stderr, deterministic exit codes. Built for agents, scripts, and CI.
- **Token-efficient.** `--format=json-minified` delivers the same schema
  with 31% fewer tokens than pretty JSON — measured, not projected.
- **Self-describing.** `meta commands` and `meta describe` expose the
  full command contract as machine-readable JSON. MCP bridge and agent
  skills are generated from the same contract model.
- **5x faster than `bq`.** A 6-step agent workflow completes in 3.2
  seconds vs 15.7 seconds with `bq`.

## Quick Start

```bash
# Build from source
go build -o dcx ./cmd/dcx

# Verify auth
dcx auth check

# Explore BigQuery
dcx datasets list --project-id=myproject
dcx tables list --project-id=myproject --dataset-id=mydataset
dcx jobs query --project-id=myproject --query="SELECT 1"

# Inspect any command's contract
dcx meta describe jobs query

# Natural-language query via CA
dcx ca ask "top errors yesterday" --profile my-spanner-profile

# Create a data agent and ask it questions
dcx ca create-agent --name=sales-agent --tables=myproject.sales.orders --project-id=myproject
dcx ca ask "revenue by region this quarter" --agent=sales-agent --project-id=myproject

# Enable shell completions (bash/zsh/fish/powershell)
source <(dcx completion bash)

# Start MCP server for agents
dcx mcp serve
```

## Commands (79 total)

| Surface | Commands |
|---|---|
| **BigQuery** | `datasets list/get/insert/delete`, `tables list/get/insert/delete`, `jobs list/get/query`, `models list/get`, `routines list/get` |
| **Spanner** | `instances list/get`, `databases list/get/get-ddl/create/drop-database/update-ddl`, `backups list/get/create/delete`, `databaseOperations list`, `instanceConfigs list/get`, `schema describe` |
| **AlloyDB** | `clusters list/get`, `instances list/get`, `backups list/get`, `users list/get`, `operations list/get`, `databases list`, `schema describe` |
| **Cloud SQL** | `instances list/get`, `databases list/get/insert/delete`, `backupRuns list/get`, `users list/get/insert/delete`, `operations list/get`, `flags list`, `tiers list`, `schema describe` |
| **Looker** | `instances list/get`, `backups list/get`, `explores list`, `dashboards get` |
| **CA** | `ca ask`, `ca create-agent`, `ca list-agents`, `ca add-verified-query` |
| **Auth** | `auth status`, `auth check` |
| **Profiles** | `profiles list`, `profiles validate`, `profiles test` |
| **Introspection** | `meta commands`, `meta describe` |
| **MCP** | `mcp serve` (JSON-RPC 2.0 / stdio, read-only, default `json-minified`) |
| **Skills** | `dcx-bigquery`, `dcx-databases`, `dcx-looker`, `dcx-ca` (checked-in) |

Run `dcx meta commands` for the full machine-readable list.

### Deferred to P1

- Agent Analytics SDK (12 commands, 6 evaluators)
- `generate-skills`, Gemini manifest
- Model Armor sanitization

## Output Format

All commands default to structured JSON. `--format` controls output:

```bash
dcx datasets list --project-id=myproject --format=json-minified
# → {"items":[{"datasetReference":{"datasetId":"events",...},...}],"source":"BigQuery"}

dcx datasets list --project-id=myproject --format=table
# → DATASET_ID    LOCATION    TYPE
#   events        US          DEFAULT
```

| Format | Use case |
|---|---|
| `json` | Human-readable, debugging |
| `json-minified` | Agent pipelines, CI (31% fewer tokens) |
| `table` | Visual scanning |
| `text` | Command-specific plain text |

Errors are structured JSON on stderr:
```json
{"error":{"code":"NOT_FOUND","message":"Dataset not found: x","hint":"Check dataset ID","exit_code":4,"retryable":false,"status":"error"}}
```

## Authentication

Five methods in priority order:

| Priority | Method | Use Case |
|----------|--------|----------|
| 1 | `DCX_TOKEN` env var / `--token` | Pre-obtained access token |
| 2 | `DCX_CREDENTIALS_FILE` / `--credentials-file` | Service account JSON |
| 3 | `dcx auth login` | Interactive OAuth (P1) |
| 4 | `GOOGLE_APPLICATION_CREDENTIALS` | Standard ADC |
| 5 | `gcloud auth application-default` | Implicit gcloud credentials |

## Benchmarks

Measured on the BigQuery 12-task parity suite (Go `dcx` vs `bq`):

| Metric | dcx | dcx (minified) | bq |
|--------|-----|----------------|-----|
| **Avg warm p50** | 518 ms | 503 ms | 2,526 ms |
| **Speedup** | **4.9x** | **5.0x** | baseline |
| **6-step workflow** | **3.2s** | **3.2s** | 15.7s |
| **Workflow tokens** | ~3,241 | **~2,239** | ~2,115 |

See [docs/benchmark_results_bigquery.md](docs/benchmark_results_bigquery.md)
for the full breakdown including per-task results, token efficiency analysis,
and Go vs Rust comparison.

```bash
# Reproduce
go build -o dcx ./cmd/dcx
benchmarks/scripts/seed_bigquery.sh YOUR_PROJECT_ID
benchmarks/scripts/run_benchmarks.sh --tasks bigquery_overlap --trials 3 --cold-trials 1
python3 benchmarks/scripts/score_results.py benchmarks/results/raw/<run-id>
```

## Architecture

### Dynamic Command Generation

dcx generates commands from bundled Google Cloud Discovery Documents
(embedded via `go:embed`). No runtime fetch, no network dependency.

| Service | Namespace | Discovery Doc | Commands |
|---------|-----------|---------------|----------|
| BigQuery | _(top-level)_ | `bigquery/v2` | datasets (CRUD), tables (CRUD), jobs, models, routines |
| Spanner | `spanner` | `spanner/v1` | instances, databases (CRUD), backups (CRUD), databaseOperations, instanceConfigs |
| AlloyDB | `alloydb` | `alloydb/v1` | clusters, instances, backups, users, operations |
| Cloud SQL | `cloudsql` | `sqladmin/v1` | instances, databases (CRUD), backupRuns, users (CRUD), operations, flags, tiers |
| Looker | `looker` | `looker/v1` | instances, backups |

### Contract System

Every command has a machine-readable contract accessible via
`dcx meta describe <command>`:

```json
{
  "contract_version": "1",
  "command": "dcx datasets list",
  "domain": "bigquery",
  "flags": [...],
  "exit_codes": {"0": "success", "2": "api_error", "3": "auth_error", "4": "not_found"},
  "supports_dry_run": false,
  "is_mutation": false
}
```

One contract model drives CLI registration, `meta describe`, MCP tool
schemas, and skill generation.

### MCP Bridge

`dcx mcp serve` exposes read-only commands as MCP tools over stdio
(JSON-RPC 2.0). Output defaults to `json-minified`; override with
`DCX_MCP_FORMAT=json`.

### Profiles

Source profiles are YAML files in `~/.config/dcx/profiles/` that configure
connection details for CA and schema commands.

```yaml
# ~/.config/dcx/profiles/finance-spanner.yaml
name: finance-spanner
source_type: spanner
project: my-gcp-project
location: us-central1
instance_id: my-spanner-instance
database_id: my-database
```

Supported source types: `bigquery`, `looker`, `looker_studio`, `alloy_db`,
`spanner`, `cloud_sql`.

## Project Structure

```
cmd/dcx/                        # entry point
internal/
  cli/                          # command tree, global flags (cobra)
  contracts/                    # CommandContract, meta commands/describe
  output/                       # render(format, value), table formatting
  errors/                       # ErrorEnvelope, ErrorCode, exit codes
  auth/                         # 5-tier resolver
  discovery/                    # Discovery Document parser, command generation
  bigquery/                     # jobs query (static), BQ client
  ca/                           # CA client (Chat + QueryData + Agent management)
  datacloud/                    # database helpers (schema describe)
  looker/                       # Looker Admin SDK client
  mcp/                          # MCP server (JSON-RPC 2.0 / stdio)
  profiles/                     # profile commands
  eval/                         # deterministic CLI eval suite (CI gate)
assets/                         # embedded Discovery JSONs (go:embed)
skills/                         # checked-in SKILL.md files
benchmarks/                     # task specs, runner, scorer
.github/workflows/ci.yml       # CI: build + test + eval gate
docs/
```

## CI

Every push and PR runs:

1. `go build` — binary compiles
2. `go test ./...` — unit tests across all packages
3. Eval suite — 11 deterministic categories validating command discovery,
   contract completeness, error handling, exit codes, help output, format
   support, auth preflight, skill alignment, and JSON contract stability

## Migration from Rust

This repo is a Go rewrite of
[`haiyuan-eng-google/bqx-cli`](https://github.com/haiyuan-eng-google/bqx-cli).

All 6 migration phases are complete:

1. **Phase 1:** Core runtime (CLI tree, output, errors, auth, `meta`) — merged
2. **Phase 2:** BigQuery P0 (Discovery pipeline, datasets, tables, query) — merged
3. **Phase 3:** Data Cloud Discovery commands (Spanner, AlloyDB, Cloud SQL, Looker) — merged
4. **Phase 4:** CA + QueryData helpers + profiles + Looker SDK — merged
5. **Phase 5:** MCP bridge + checked-in skills — merged
6. **Phase 6:** Benchmark hardening + eval suite + CI gate — merged

See [docs/go_mvp_plan.md](docs/go_mvp_plan.md) for the full plan with
success criteria, implementation types, and dependency analysis.

## License

Internal — Google Cloud.
