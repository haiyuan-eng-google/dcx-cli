# dcx — Agent-Native Data Cloud CLI

An agent-native CLI for Google Cloud's Data Cloud, built in Go.
One binary for BigQuery, Spanner, AlloyDB, Cloud SQL, and Looker —
with structured output, typed errors, and an MCP bridge for AI agents.

> **Status:** Go rewrite in progress. The reference Rust implementation
> lives at [`haiyuan-eng-google/bqx-cli`](https://github.com/haiyuan-eng-google/bqx-cli).
> See [docs/go_mvp_plan.md](docs/go_mvp_plan.md) for the migration plan.

## Why dcx

- **One CLI for five services.** BigQuery, Spanner, AlloyDB, Cloud SQL,
  and Looker through a single binary — no per-service tooling.
- **Machine-safe output.** Structured JSON on stdout, typed errors on
  stderr, deterministic exit codes. Built for agents, scripts, and CI.
- **Token-efficient.** `--format=json-minified` delivers the same schema
  with 27% fewer tokens than pretty JSON — measured, not projected.
- **Self-describing.** `meta commands` and `meta describe` expose the
  full command contract as machine-readable JSON. MCP bridge and agent
  skills are generated from the same contract model.

## Quick Start

> **Not yet functional.** The Go implementation is in progress.
> These commands show the target interface — see
> [docs/go_mvp_plan.md](docs/go_mvp_plan.md) for current status.

```bash
# Build from source (stub only — exits with "not yet implemented")
go build -o dcx ./cmd/dcx

# Target interface (Phase 1–5):
dcx auth check                # verify credentials
dcx datasets list --project-id=myproject
dcx jobs query --project-id=myproject --query="SELECT 1"
dcx meta describe jobs query  # inspect any command's contract
dcx mcp serve                 # start MCP server for agents
```

For a working CLI today, use the Rust reference implementation:
[`haiyuan-eng-google/bqx-cli`](https://github.com/haiyuan-eng-google/bqx-cli).

## MVP Scope

This Go implementation targets the smallest `dcx` that preserves the
current product wedge. See [docs/go_mvp_plan.md](docs/go_mvp_plan.md)
for the full plan.

### P0: What ships

| Surface | Commands |
|---|---|
| **Core runtime** | `--format json\|json-minified\|table\|text`, structured errors, semantic exit codes, 5-tier auth |
| **BigQuery** | `datasets list/get`, `tables list/get`, `jobs query`, `jobs query --dry-run` |
| **Spanner** | `instances list`, `databases list/get-ddl`, `schema describe` |
| **AlloyDB** | `clusters list`, `instances list`, `databases list`, `schema describe` |
| **Cloud SQL** | `instances list`, `databases list`, `schema describe` |
| **Looker** | `instances list`, `explores list`, `dashboards get` |
| **CA** | `ca ask --profile ...` (natural-language queries across all sources) |
| **Auth** | `auth status`, `auth check` |
| **Profiles** | `profiles list`, `profiles validate`, `profiles test` |
| **Introspection** | `meta commands`, `meta describe` |
| **MCP** | `mcp serve` (JSON-RPC 2.0 / stdio, read-only, default `json-minified`) |
| **Skills** | `dcx-bigquery`, `dcx-databases`, `dcx-looker`, `dcx-ca` (checked-in) |

### Deferred to P1

- Agent Analytics SDK (12 commands, 6 evaluators)
- CA agent management (`create-agent`, `list-agents`, `add-verified-query`)
- `generate-skills`, Gemini manifest, shell completions
- Model Armor sanitization

## Output Format (target contract)

All commands will default to structured JSON. `--format` controls output.
These examples show the target output shape from the
[Rust reference implementation](https://github.com/haiyuan-eng-google/bqx-cli):

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
| `json-minified` | Agent pipelines, CI (27% fewer tokens) |
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
| 3 | `dcx auth login` | Interactive OAuth |
| 4 | `GOOGLE_APPLICATION_CREDENTIALS` | Standard ADC |
| 5 | `gcloud auth application-default` | Implicit gcloud credentials |

## Architecture

### Dynamic Command Generation

dcx generates commands from bundled Google Cloud Discovery Documents at
build time (embedded via `go:embed`). No runtime fetch, no network dependency.

| Service | Namespace | Discovery Doc | Commands |
|---------|-----------|---------------|----------|
| BigQuery | _(top-level)_ | `bigquery/v2` | datasets, tables, routines, models |
| Spanner | `spanner` | `spanner/v1` | instances, databases, getDdl |
| AlloyDB | `alloydb` | `alloydb/v1` | clusters, instances |
| Cloud SQL | `cloudsql` | `sqladmin/v1` | instances, databases |
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
  "supports_dry_run": true,
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

## Benchmarks

Benchmark suite carried over from the Rust implementation. Compares dcx
against `bq` and `gcloud` on real workflows.

| Track | Tasks | Measured baseline (Rust) |
|-------|-------|------------------------|
| BigQuery parity | 12 tasks x 3 CLIs | 4.8x faster than `bq`, 27% fewer tokens |
| Spanner parity | 11 tasks x 2 CLIs | 1.3-3.9x faster on error-handling tasks |
| dcx differentiated | 8 tasks | 7/8 pass, avg 141 ms |

```bash
benchmarks/scripts/run_benchmarks.sh --tasks bigquery_overlap --trials 3 --cold-trials 1
python3 benchmarks/scripts/score_results.py benchmarks/results/raw/<run-id>
```

The Go implementation must meet or beat these numbers before MVP release.
See [docs/go_mvp_plan.md](docs/go_mvp_plan.md) for success criteria.

## Project Structure

```
cmd/dcx/                        # entry point
internal/
  cli/                          # command tree, global flags (cobra)
  contracts/                    # CommandContract, meta commands/describe
  output/                       # render(format, value), table formatting
  errors/                       # ErrorEnvelope, ErrorCode, exit codes
  auth/                         # 5-tier resolver, OAuth login, keyring
  discovery/                    # Discovery Document parser, command generation
  bigquery/                     # jobs query (static), BQ client
  ca/                           # CA client (Chat + QueryData), profiles
  datacloud/                    # database helpers (schema describe)
  looker/                       # Looker Admin SDK client
  mcp/                          # MCP server (JSON-RPC 2.0 / stdio)
  profiles/                     # profile commands
  skills/                       # skill templates
assets/                         # embedded Discovery JSONs (go:embed)
skills/                         # checked-in SKILL.md files
benchmarks/                     # task specs, runner, scorer
docs/
```

## Migration from Rust

This repo is a Go rewrite of
[`haiyuan-eng-google/bqx-cli`](https://github.com/haiyuan-eng-google/bqx-cli).
The Rust implementation remains the source of truth during migration.

Migration phases:

1. **Phase 1:** Core runtime (CLI tree, output, errors, auth, `meta`)
2. **Phase 2:** BigQuery P0 (Discovery pipeline, datasets, tables, query)
3. **Phase 3:** Data Cloud Discovery commands (Spanner, AlloyDB, Cloud SQL, Looker)
4. **Phase 4:** CA + QueryData helpers + profiles + Looker SDK
5. **Phase 5:** MCP bridge + checked-in skills
6. **Phase 6:** Benchmark hardening + release

See [docs/go_mvp_plan.md](docs/go_mvp_plan.md) for the full plan with
success criteria, implementation types, and dependency analysis.

## License

Internal — Google Cloud.
