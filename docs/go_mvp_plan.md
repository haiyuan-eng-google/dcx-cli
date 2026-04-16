# Plan: Export `bqx-cli` to `dcx-cli` in Go

## Goal

Create a new Go implementation in
[`haiyuan-eng-google/dcx-cli`](https://github.com/haiyuan-eng-google/dcx-cli)
that preserves the current agent-first value of `bqx-cli` while reducing
rewrite scope to a measurable MVP.

This is not a line-by-line port. The goal is to ship the smallest Go-based
`dcx` that is:

- useful to AI agent users on day one
- contract-compatible with the current CLI where it matters
- benchmarked against both `bq` and the current Rust implementation

## Product Thesis

For the Go rewrite, the wedge should be broader Data Cloud access, not the
analytics bundle.

The original design
([design-dcx-cli-agent-first-bundle.md](https://github.com/haiyuan-eng-google/bigquery-agent-analytics-blogpost/blob/main/design-dcx-cli-agent-first-bundle.md))
positioned analytics + CA as the agent-first wedge. The Go MVP pivots to
Data Cloud breadth instead, because "one CLI across five services" is a
stronger differentiation for an MVP than "analytics dashboard CLI". Analytics
depth remains important but moves to the next phase.

The MVP should preserve these differentiators:

- one CLI across BigQuery, Spanner, AlloyDB, Cloud SQL, and Looker
- conversational access aligned to those same Data Cloud sources
- self-describing command contracts and MCP bridge for agent runtimes
- machine-safe output, typed errors, and token-efficient JSON formats

Analytics remains important, but it should move to the next phase instead of
being part of the first Go MVP.

## MVP Principle

Port the surfaces that make `dcx` meaningfully different from `bq`.
Defer breadth that is useful but not part of the core pull.

If a feature is mainly "more coverage of Google Cloud resources" rather than
"why an agent would install dcx instead of using bq/gcloud", it is not MVP.

## MVP Scope

### P0: Core runtime contract

These are mandatory. Without them, the Go port is not `dcx`, just another CLI.

| Area | MVP requirement |
|---|---|
| Output formats | `json`, `json-minified`, `table`, `text` |
| Error model | Structured stderr envelope, typed error codes, semantic exit codes |
| Auth | ADC, explicit bearer token, service-account credentials file |
| Global flags | `--format`, `--project-id`, `--dataset-id`, `--location`, `--token`, `--credentials-file`, `--dry-run` where supported |
| Contract | `meta commands`, `meta describe` |
| Pagination | normalized `items` envelope, `--page-token`, `--page-all` where supported |
| MCP | `mcp serve` with read-only tool surface, default `json-minified` |

### P0: BigQuery core commands

These commands are the minimum required for the "agent-native BigQuery CLI"
claim and for benchmark continuity.

| Resource | Commands | Implementation type |
|---|---|---|
| datasets | `list`, `get` | Discovery-driven (dynamic) |
| tables | `list`, `get` | Discovery-driven (dynamic) |
| jobs | `query`, `query --dry-run` | Static (BigQuery API client) |

Implementation note: In the Rust codebase, `datasets` and `tables` commands
are generated from the BigQuery v2 Discovery Document via the dynamic
pipeline (`bigquery/dynamic/`). `jobs query` is a hand-written static
command (`commands/jobs_query.rs`) because query execution requires request
body construction that does not fit the generic Discovery GET pattern. The
Go port must handle both paths.

Rationale:

- these are the commands used in the current BigQuery parity benchmark
  (12 tasks in `benchmarks/tasks/bigquery_overlap.yaml`)
- they cover exploration, schema inspection, dry-run, and query execution
- they are enough for the current 6-step benchmark workflow

### P0: Data Cloud core commands

These are the core multi-source commands that make `dcx` more than a
BigQuery-only wrapper.

| Surface | Commands | Implementation type |
|---|---|---|
| Spanner | `instances list`, `databases list`, `databases get-ddl` | Discovery-driven |
| Spanner | `schema describe` | QueryData helper (requires CA client) |
| AlloyDB | `clusters list`, `instances list` | Discovery-driven |
| AlloyDB | `databases list`, `schema describe` | QueryData helper (requires CA client) |
| Cloud SQL | `instances list` | Discovery-driven |
| Cloud SQL | `databases list`, `schema describe` | QueryData helper (requires CA client) |
| Looker | `instances list` | Discovery-driven |
| Looker | `explores list`, `dashboards get` | Looker Admin SDK (custom client) |

Implementation note: Three distinct implementation types exist here:

1. **Discovery-driven**: Spanner instances/databases, AlloyDB clusters/
   instances, Cloud SQL instances, Looker instances. These go through the
   same generic pipeline as BigQuery dynamic commands — load Discovery
   Document, extract methods, filter to allowlist, generate commands.

2. **QueryData helpers**: `schema describe` and AlloyDB/Cloud SQL
   `databases list`. In the Rust codebase these are implemented in
   `commands/database_helpers.rs` (797 lines) and use
   `CaClient::ask_querydata()` to execute `INFORMATION_SCHEMA` queries
   through the CA QueryData API. This creates a hard dependency: the CA
   client (at least the QueryData path) must be available before these
   commands can ship.

3. **Looker Admin SDK**: `explores list/get` and `dashboards list/get`
   use a custom Looker client (`sources/looker/client.rs`, 351 lines),
   not the Discovery pipeline. This requires a separate HTTP client
   implementation.

Rationale:

- these are the current cross-source surfaces that support real Data Cloud
  workflows
- they align directly with CA source profiles
- they give the Go repo a clear value proposition beyond "BigQuery in Go"

### P0: Conversational Analytics core

CA scope should align to the same Data Cloud source families shipped in the
CLI MVP.

| Command | Why P0 |
|---|---|
| `ca ask --profile ...` | natural-language wedge across BigQuery, Spanner, AlloyDB, Cloud SQL, and Looker |

The CA client also provides the QueryData API used by `schema describe` and
AlloyDB/Cloud SQL `databases list` helpers. The QueryData path of the CA
client is therefore a hidden P0 dependency even if `ca ask` itself were
deferred.

For MVP, prioritize the profile-driven path over inline BigQuery-only agent
management flows.

`ca create-agent`, `ca list-agents`, and `ca add-verified-query` — **shipped**
(merged in PR #7). Agent management uses `locations/global`; the `--location`
flag is ignored for these commands.

### P0: Auth and profiles

| Command | Why P0 |
|---|---|
| `auth status` | local auth inspection |
| `auth check` | CI/agent preflight |
| `profiles list` | source discovery |
| `profiles validate` | profile sanity check |
| `profiles test` | source + auth preflight for real Data Cloud profiles |

### P0: Skills bundle

Ship checked-in skills, not skill generation.

Minimum checked-in skills:

- `dcx-bigquery`
- `dcx-databases`
- `dcx-looker`
- `dcx-ca`

These should be generated from the same command contract during release/build,
but the `generate-skills` command itself is not required for MVP.

## Explicitly Deferred from MVP

These should not block the Go repo launch.

### P1: Breadth beyond the wedge

- Additional Spanner dynamic commands (beyond allowlist)
- Additional AlloyDB dynamic commands
- Additional Cloud SQL dynamic commands
- Additional Looker dynamic commands
- Looker content helpers (beyond `explores list`, `dashboards get`)

### P1: Analytics surface (12 commands)

- `analytics doctor`
- `analytics evaluate` (6 evaluator types)
- `analytics get-trace`
- `analytics list-traces`
- `analytics drift`
- `analytics insights`
- `analytics distribution`
- `analytics hitl-metrics`
- `analytics views create`
- `analytics views create-all`
- `analytics categorical-eval`
- `analytics categorical-views`

Note: Analytics was the original wedge in the Agent-First Bundle design.
It moves to P1 because the Data Cloud breadth story is a stronger
differentiator for the Go MVP. Analytics adds depth for existing BigQuery
agent users, but it does not require a rewrite to deliver value — the Rust
CLI can continue serving this surface during the Go migration.

### P1: Management and generation helpers

- ~~`generate-skills`~~ — **shipped** (PR #24, as `meta generate-skills`)
- Gemini manifest generation (`meta gemini-tools`)
- ~~shell completions~~ — **shipped** (PR #15)
- ~~`ca create-agent`, `ca list-agents`, `ca add-verified-query`~~ — **shipped** (PR #7)
- Model Armor sanitization (`--sanitize`)

## Why this MVP, specifically

This scope keeps the Go rewrite aligned with the current strongest proof
points:

1. BigQuery parity benchmark
2. multi-source Data Cloud access beyond `bq`
3. natural-language entry point (`ca ask`) aligned to those same sources
4. agent-runtime integration (`meta`, MCP, skills)

That gives the Go repo a product story on day one:

> "Install dcx, your agent can inspect BigQuery, Spanner, AlloyDB, Cloud SQL,
> and Looker, and ask natural-language questions across those sources, with
> structured output and lower token cost than `bq`."

## Rust Codebase Inventory (reference for porting)

Key modules and estimated porting effort:

| Module | Rust lines | Key files | Go equivalent |
|---|---|---|---|
| CLI tree + global flags | ~580 | `cli.rs` | `internal/cli/` (cobra) |
| Entry point + router | ~870 | `main.rs` | `cmd/dcx/` |
| Output rendering | ~1,340 | `output.rs` | `internal/output/` |
| Error model + exit codes | ~440 | `models.rs` | `internal/errors/` |
| Discovery pipeline | ~2,660 | `bigquery/dynamic/` | `internal/discovery/` |
| Auth (5-tier resolution) | ~910 | `auth/` | `internal/auth/` |
| Contract system | ~1,440 | `commands/meta.rs` | `internal/contracts/` |
| MCP bridge | ~660 | `commands/mcp.rs` | `internal/mcp/` |
| CA client + profiles | ~3,130 | `ca/` | `internal/ca/` |
| Database helpers | ~800 | `commands/database_helpers.rs` | `internal/datacloud/helpers.go` |
| Jobs query | ~190 | `commands/jobs_query.rs` | `internal/bigquery/query.go` |
| Looker client | ~510 | `sources/looker/` | `internal/looker/` |
| Profiles commands | ~200 | `commands/profiles/` | `internal/profiles/` |
| Auth commands | ~410 | `auth/login.rs` | `internal/auth/commands.go` |
| Skill templates | ~450 | `skills/` | `internal/skills/` |
| **Total P0 source** | **~14,600** | | |
| Analytics (P1) | ~4,170 | `commands/analytics/` | deferred |
| CA management | ~400 | `commands/ca/` (3 cmds) | `internal/ca/` + `internal/cli/ca.go` — **shipped** |
| Tests | ~8,000+ | `tests/` | `*_test.go` alongside packages |
| Snapshot golden files | ~50 files | `tests/snapshots/` | `testdata/` |

### Discovery Documents (bundled assets)

These JSON files are compiled into the Rust binary via `include_str!()`.
The Go port should embed them via `go:embed`.

| Service | File | Size | Allowed methods |
|---|---|---|---|
| BigQuery | `assets/bigquery_v2_discovery.json` | 527 KB | 8 (datasets, tables, routines, models × list/get) |
| Spanner | `assets/spanner_v1_discovery.json` | 479 KB | 5 (instances list/get, databases list/get/getDdl) |
| AlloyDB | `assets/alloydb_v1_discovery.json` | 326 KB | 4 (clusters list/get, instances list/get) |
| Cloud SQL | `assets/sqladmin_v1_discovery.json` | 360 KB | 4 (instances list/get, databases list/get) |
| Looker | `assets/looker_v1_discovery.json` | 68 KB | 4 (instances list/get, backups list/get) |

### Auth Resolution Priority (must match exactly)

1. `DCX_TOKEN` env var / `--token` flag (static bearer token)
2. `DCX_CREDENTIALS_FILE` env var / `--credentials-file` (service account JSON)
3. Stored `dcx auth login` credentials (OAuth refresh_token in keyring)
4. `GOOGLE_APPLICATION_CREDENTIALS` (standard ADC file)
5. Default ADC (gcloud / metadata server)

### Error Codes and Exit Codes (must match exactly)

| Exit code | Meaning | HTTP triggers |
|---|---|---|
| 0 | success | — |
| 1 | validation / eval failure | — |
| 2 | infrastructure / API error | 500, 502, 503, 504 |
| 3 | auth error | 401, 403 |
| 4 | not found | 404 |
| 5 | conflict | 409 |

Error envelope (stderr JSON):
```json
{"error":{"code":"API_ERROR","message":"...","hint":"...","exit_code":2,"retryable":true,"status":"error"}}
```

Error code enum: `MISSING_ARGUMENT`, `INVALID_IDENTIFIER`, `INVALID_CONFIG`,
`UNKNOWN_COMMAND`, `AUTH_ERROR`, `API_ERROR`, `NOT_FOUND`, `CONFLICT`,
`EVAL_FAILED`, `INFRA_ERROR`, `INTERNAL`.

## Benchmark and Evaluation Scope in `dcx-cli`

The Go repo must include benchmarking from the start. Do not treat it as
follow-up work.

### Benchmark assets to carry over

Copy these from `bqx-cli` into `dcx-cli` early:

| Asset | Path | Description |
|---|---|---|
| BigQuery parity tasks | `benchmarks/tasks/bigquery_overlap.yaml` | 12 tasks × 3 CLI variants (dcx, dcx-minified, bq) |
| Spanner parity tasks | `benchmarks/tasks/spanner_overlap.yaml` | 11 tasks × 2 CLI variants (dcx, gcloud) |
| Differentiated tasks | `benchmarks/tasks/dcx_differentiated.yaml` | 8 tasks (meta, dry-run, auth, MCP, skills) |
| Runner script | `benchmarks/scripts/run_benchmarks.sh` | Bash runner (cold + warm trials, NDJSON output) |
| Scorer | `benchmarks/scripts/score_results.py` | Python scorecard generator |
| Seed scripts | `benchmarks/scripts/seed_bigquery.sh`, `seed_spanner.sh` | Test data setup |
| Seed SQL | `benchmarks/data/bigquery/seed.sql`, `benchmarks/data/spanner/` | DDL + DML |
| README | `benchmarks/README.md` | Runner usage and task spec format |

Also copy or regenerate:

- `docs/benchmark_results_bigquery.md` (3-CLI comparison with measured data)
- `docs/cli_benchmark_plan.md` (methodology and scoring model)

### Required benchmark tracks in the Go repo

#### 1. BigQuery parity benchmark

Keep the current 12-task BigQuery suite and compare:

- `bq`
- current Rust `dcx`
- new Go `dcx`

This is the most important migration benchmark because it measures:

- latency (wall clock ms, cold + warm trials)
- correctness (exit code 0, expected JSON keys)
- stdout byte cost / token cost (÷4 approximation)
- error-path quality (structured stderr, semantic exit codes)

Current measured baseline (run `20260411-013709-b4c8ac5`):

| Metric | dcx (json) | dcx (json-minified) | bq |
|---|---:|---:|---:|
| 6-step workflow bytes | 11,436 B | 8,319 B | 8,461 B |
| Est. tokens (÷4) | ~2,859 | ~2,080 | ~2,115 |
| Avg p50 ratio vs bq | 4.8x faster | 4.8x faster | 1.0x |

The Go `dcx` must match or beat these numbers.

#### 2. Deterministic CLI eval suite

Build a Go-native eval suite covering these categories:

| Category | What it tests | Source |
|---|---|---|
| command discovery | `meta commands` returns all P0 commands | new |
| contract completeness | `meta describe` has flags, exit codes, formats for all P0 | new |
| dry-run success | `--dry-run` returns structured output without network | existing (`dcx_differentiated.yaml`) |
| error recovery | invalid args produce typed error envelope on stderr | new |
| exit-code semantics | 401→3, 404→4, 409→5, validation→1 | new |
| JSON contract stability | top-level keys match Rust snapshots | new |
| help completeness | `--help` works for all P0 commands | new |
| format support | `--format json/json-minified/table/text` all produce output | new |
| preflight validation | `auth check` returns structured JSON | existing (`dcx_differentiated.yaml`) |
| auth preflight | missing credentials → exit 3 + error envelope | new |
| skill alignment | checked-in skills reference valid P0 commands | new |

The existing `dcx_differentiated.yaml` covers 3 of these categories.
The remaining 8 should be implemented as Go test functions that run `dcx`
as a subprocess and validate output shape.

This should become a CI gate.

#### 3. Rust parity regression suite

During migration only, add a compatibility test layer that compares Go `dcx`
against the current Rust `dcx` for the MVP command set.

Required comparisons:

- exit codes match for same inputs
- top-level JSON keys match for all P0 commands
- `meta describe` shape matches (same flags, same exit codes, same formats)
- `json-minified` byte counts within 10% tolerance
- profile-driven namespace helper output and exit behavior match

This temporary suite can be removed after the Go implementation becomes the
source of truth.

### Benchmark success criteria for the Go MVP

The Go port is ready for public MVP only if all of these hold:

#### Contract correctness

- all P0 commands exist and are registered with the contract system
- `meta describe` reports the same command families and format values
- structured errors and exit-code semantics match the Rust contract for P0
- error envelope on stderr has same top-level keys (`code`, `message`,
  `hint`, `exit_code`, `retryable`, `status`)

#### BigQuery benchmark bar

- correctness: no worse than current Rust `dcx` on the 12-task BigQuery suite
- avg p50: within 10% of current Rust `dcx` on the same machine/project
- token cost: `json-minified` remains at or below `bq` on the 6-step workflow

#### Data Cloud breadth bar

- all P0 namespace helpers return machine-safe output
- Discovery-driven commands for all 5 services produce `items` envelope
- profile-driven commands work for at least one fixture/source in each family:
  Spanner, AlloyDB, Cloud SQL, Looker
- `ca ask --profile ...` works for at least:
  - one BigQuery profile
  - one database profile (`spanner` or `alloydb` or `cloud_sql`)
  - one Looker profile

#### Agent usability bar

- MCP `tools/list` and `tools/call` work on the P0 read-only surface
- MCP defaults to `json-minified`, overridable via `DCX_MCP_FORMAT`
- checked-in skills align with the P0 contract
- `auth check` and `meta describe` remain machine-safe

## Proposed Go architecture

Use one contract model and generate everything else from it.

### Repo layout

```text
cmd/dcx/                        # entry point
internal/
  cli/                          # command tree, global flags (cobra)
  contracts/                    # CommandContract, FlagContract, collect/describe
  output/                       # render(format, value), table formatting
  errors/                       # ErrorEnvelope, ErrorCode, exit codes
  auth/                         # 5-tier resolver, OAuth login, keyring store
  discovery/                    # Discovery Document parser, method extraction
    service.go                  # per-service config (BQ, Spanner, AlloyDB, ...)
    model.go                    # ApiMethod, GeneratedCommand, ApiParam
    claptree.go                 # cobra command builder from GeneratedCommand
    executor.go                 # execute: validate → auth → request → render
    request.go                  # URL builder, path param substitution
  bigquery/                     # jobs query (static), BQ-specific client
  ca/                           # CA client (Chat API + QueryData API)
    client.go                   # ask_question(), ask_querydata()
    profiles.go                 # CaProfile, SourceType, YAML parsing
    models.go                   # request/response types
  datacloud/                    # database helpers (schema describe, databases list)
  looker/                       # Looker Admin SDK client
  mcp/                          # MCP server (JSON-RPC 2.0 / stdio)
  profiles/                     # profiles list/show/validate/test commands
  skills/                       # skill templates, generation
assets/                         # embedded Discovery JSONs (go:embed)
  bigquery_v2_discovery.json
  spanner_v1_discovery.json
  alloydb_v1_discovery.json
  sqladmin_v1_discovery.json
  looker_v1_discovery.json
skills/                         # checked-in skill SKILL.md files
benchmarks/                     # copied from bqx-cli
docs/
```

### Design rules

1. **One contract model, not two.**
   Do not hand-define Cobra commands and then separately hand-define
   contracts. The Go repo should have one command spec model that drives:
   - CLI registration (cobra commands)
   - `meta commands` / `meta describe`
   - skill generation
   - MCP tool schemas

2. **Centralized output and error rendering.**
   The Rust repo already showed that agent quality depends on a consistent
   stdout/stderr split and typed errors. The Go port must have a single
   `output.Render(format, value)` function and a single
   `errors.Emit(code, message, hint)` function.

3. **Discovery-driven commands stay declarative.**
   The Go port should keep the Discovery-driven command generation approach.
   Do not regress to manually coded one-off commands for BigQuery
   datasets/tables or Spanner/AlloyDB/Cloud SQL/Looker Discovery resources.

4. **Unified contract model for all command types.**
   The Go repo should not have one contract system for BigQuery and a second
   ad hoc system for Spanner/AlloyDB/Cloud SQL/Looker. Discovery commands,
   static commands (jobs query), and helper commands (schema describe) all
   register through the same contract interface.

5. **`json-minified` and MCP behavior are first-class.**
   MCP defaults to `json-minified` (validated via `DCX_MCP_FORMAT`
   allowlist). Do not re-add pretty JSON defaults inside machine-oriented
   paths.

6. **Use `go:embed` for Discovery Documents.**
   Bundled JSON files compiled into the binary. No runtime fetch, no
   network dependency. Same approach as Rust `include_str!()`.

### Go library recommendations

| Concern | Recommended | Why |
|---|---|---|
| CLI framework | `cobra` + `pflag` | Standard Go CLI. Matches Rust clap's subcommand model. |
| HTTP client | `net/http` | stdlib is sufficient; no need for external HTTP lib |
| JSON | `encoding/json` | stdlib; use `json.MarshalIndent` for pretty, `json.Marshal` for minified |
| Auth | `golang.org/x/oauth2/google` | Standard Google auth. Covers ADC, service account, OAuth refresh. |
| Keyring | `github.com/zalando/go-keyring` | Cross-platform (macOS Keychain, Windows, Linux). |
| Table output | `github.com/olekukonko/tablewriter` | UTF-8 table formatting. |
| YAML profiles | `gopkg.in/yaml.v3` | Profile parsing. |
| Embed | `embed` (stdlib) | Discovery JSON and asset files. |
| Testing | `testing` + `testify` | Standard Go testing with assertions. |

## Migration phases

### Phase 0: Freeze the Rust contract

Before building the Go repo:

- export `meta commands` output from current Rust `dcx`
- export `meta describe` for every P0 command
- snapshot representative stdout/stderr for P0 commands in:
  - `json`
  - `json-minified`
  - `table` where relevant

These become the migration oracle. Store them in `dcx-cli/testdata/rust-snapshots/`.

Deliverables:
- `testdata/rust-snapshots/meta-commands.json`
- `testdata/rust-snapshots/meta-describe-*.json` (one per P0 command)
- `testdata/rust-snapshots/*.stdout.json` (representative outputs)
- `testdata/rust-snapshots/*.stderr.json` (error cases)

### Phase 1: Bootstrap Go core

Deliver:

- command tree (cobra) with global flags
- output formats (`json`, `json-minified`, `table`, `text`)
- error envelope on stderr (typed codes, semantic exit codes)
- auth resolution (5-tier priority chain)
- `meta commands`
- `meta describe`

Exit criterion:

- Go `meta describe` matches Rust snapshots for a small P0 sample set
- `--format` flag accepted for all four values
- error envelope shape matches Rust contract

Estimated scope: ~2,800 lines of Go (cli + contracts + output + errors + auth)

### Phase 2: BigQuery P0

Deliver:

- Discovery pipeline (load → extract → filter → generate → execute)
- `datasets list/get` (Discovery-driven)
- `tables list/get` (Discovery-driven)
- `jobs query` (static, BigQuery API client)
- `jobs query --dry-run`
- normalized list envelope (`items`, `source`, `next_page_token`)
- pagination support (`--page-token`, `--page-all`)

Exit criterion:

- BigQuery parity benchmark runs against `bq`, Rust `dcx`, and Go `dcx`
- All 12 tasks in `bigquery_overlap.yaml` pass validation
- `json-minified` byte counts within 10% of Rust `dcx`

Estimated scope: ~3,200 lines of Go (discovery pipeline + BQ client + query)

### Phase 3: Data Cloud Discovery commands

Deliver Discovery-driven commands only (no QueryData dependency):

- Spanner: `instances list`, `databases list`, `databases get-ddl`
- AlloyDB: `clusters list`, `instances list`
- Cloud SQL: `instances list`
- Looker: `instances list`

This phase reuses the Discovery pipeline from Phase 2 with per-service
configs. Each service needs:
- `ServiceConfig` with namespace, bundled JSON, allowed methods, global
  param mappings
- `flatPath` vs `path` URL construction (Spanner/AlloyDB use `flatPath`)

Exit criterion:

- All Discovery-driven Data Cloud commands produce `items` envelope
- Spanner parity tasks (`spanner_overlap.yaml`) pass for Discovery commands
- `meta describe` includes all shipped commands

Estimated scope: ~400 lines of Go (service configs + integration tests)

### Phase 4: CA + QueryData helpers + profiles

Deliver:

- CA client (Chat API for `ca ask`, QueryData API for helpers)
- Profile system (YAML parsing, source type resolution, validation)
- `ca ask --profile ...`
- `schema describe` for Spanner, AlloyDB, Cloud SQL (QueryData-driven)
- `databases list` for AlloyDB, Cloud SQL (QueryData-driven)
- Looker Admin SDK client for `explores list`, `dashboards get`
- `auth status`, `auth check`
- `profiles list`, `profiles validate`, `profiles test`

Note: This phase is larger than originally planned because it absorbs the
QueryData-dependent helpers that were in Phase 3. The dependency chain is:
`schema describe` → `CaClient::ask_querydata()` → CA QueryData API →
profile resolution → auth. All of these must ship together.

Exit criterion:

- first-run multi-source agent workflow works with checked-in skills
- profile-driven commands work for at least one source per family
- `ca ask` works for BigQuery, Spanner/AlloyDB/Cloud SQL, and Looker profiles
- Looker `explores list` and `dashboards get` return structured output

Estimated scope: ~4,500 lines of Go (CA client + profiles + helpers + Looker)

### Phase 5: MCP + skills bundle

Deliver:

- `mcp serve` (JSON-RPC 2.0 over stdio)
- read-only tool subset from P0 surface
- `DCX_MCP_FORMAT` validation (allowlist: `json`, `json-minified`)
- checked-in skills: `dcx-bigquery`, `dcx-databases`, `dcx-looker`, `dcx-ca`

MCP implementation note: The Rust MCP bridge uses subprocess isolation —
each `tools/call` spawns a `dcx` subprocess with args. This ensures
contract parity with the CLI. The Go port should use the same approach
for MVP. In-process execution can be optimized later.

Exit criterion:

- MCP `tools/list` returns all P0 read-only commands
- MCP `tools/call` succeeds on P0 commands
- skill docs reference valid P0 commands only
- `DCX_MCP_FORMAT=invalid` fails at startup with clear error

Estimated scope: ~800 lines of Go (MCP server + skill files)

### Phase 6: Benchmark hardening and release

Deliver:

- Deterministic CLI eval suite (11 categories, Go test functions)
- Rust parity regression suite (snapshot comparison)
- BigQuery benchmark run with Go `dcx` results
- benchmark docs refreshed from Go results
- CI jobs for deterministic evals
- release packaging for `npx dcx`

Exit criterion:

- Go repo meets all benchmark success criteria above
- Deterministic eval suite is a CI gate (all 11 categories pass)
- public MVP release can point to measured results, not projections

Estimated scope: ~1,500 lines of Go tests + CI config + docs

### Total estimated Go MVP

~13,200 lines of Go source + ~1,500 lines of test/benchmark code.

## Repo-to-repo migration strategy

Do not freeze the Rust repo immediately.

Recommended approach:

1. `bqx-cli` remains the source of truth during migration
2. `dcx-cli` starts as a Go rewrite repo with copied benchmark/docs assets
3. every Phase 1–5 milestone is compared against Rust `dcx` snapshots
4. once the Go repo reaches MVP parity, flip product messaging and release
   focus to `dcx-cli`
5. keep `bqx-cli` as the compatibility/reference repo until at least one
   public Go release is stable

### Assets to copy on day one

From `bqx-cli` to `dcx-cli`:

```
benchmarks/                          # full directory
docs/benchmark_results_bigquery.md
docs/cli_benchmark_plan.md
docs/dcx-vs-bq.md
docs/source-matrix.md
skills/dcx-bigquery/                 # checked-in skills
skills/dcx-databases/
skills/dcx-looker/
skills/dcx-ca/
assets/*.json                        # Discovery Documents
```

## Recommended first release label

Use a clearly scoped MVP label in the new repo, for example:

- `v0.1.0-go-mvp`

Do not imply full parity with the current Rust CLI in the first Go release.
The right message is:

> "Go MVP of dcx: BigQuery + Data Cloud source commands + CA ask + meta/MCP + CLI benchmark suite."

## Implementation status

All 6 phases are complete. The Go MVP is functional with 82 commands
across 11 domains, benchmarked at 5x faster than `bq`.

| Phase | Status | PR |
|-------|--------|-----|
| Phase 1: Core runtime | Merged | #2 |
| Phase 2: BigQuery P0 | Merged | #2 |
| Phase 3: Data Cloud Discovery | Merged | #2 |
| Phase 4: CA + QueryData + Looker | Merged | #3 |
| Phase 5: MCP + skills | Merged | #2 |
| Phase 6: Eval suite + CI | Merged | #4 |
| Benchmark run + docs | Merged | #5 |

## Bottom line

The Go MVP is not "all of current `bqx-cli` in another language."

It is:

- BigQuery core (Discovery + static query)
- Data Cloud source core (Discovery + QueryData helpers + Looker SDK)
- `ca ask` (profile-driven, multi-source)
- auth/profile basics (5-tier resolution, YAML profiles)
- `meta describe` (single contract model)
- MCP bridge (subprocess isolation, `json-minified` default)
- checked-in skills (4 skill bundles)
- the benchmark/eval system that proves it did not regress

That is the smallest Go `dcx` that still preserves the current product wedge.
