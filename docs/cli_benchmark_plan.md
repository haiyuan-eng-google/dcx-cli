# CLI Benchmark Plan: `dcx` vs `bq` and Spanner CLI

This document defines a systematic benchmark for comparing `dcx` against the
existing Google Cloud CLIs for overlapping BigQuery and Spanner tasks.

The goal is to benchmark **CLI-mediated task completion**, not raw backend
database performance. If the benchmark only measures query latency, it mostly
measures BigQuery or Spanner, not the CLI layer.

## 1. Goals

This benchmark should answer four questions:

1. For overlapping tasks, how does `dcx` compare with `bq` and Spanner CLI on
   correctness, latency, and operational reliability?
2. For agent-facing workflows, does `dcx` reduce command count, retries, and
   parsing burden?
3. Do `dcx` machine contracts improve unattended execution:
   structured output, exit codes, dry-run, pagination, and discovery?
4. Where does `dcx` provide differentiated value that should be measured
   separately rather than mixed into a parity score?

## 2. Design Principles

### 2.1 Separate overlap from differentiated value

Use two scoreboards:

- **Parity benchmark**
  Compare only overlapping surfaces:
  - BigQuery dataset/table/query/jobs/models/routines tasks
  - Spanner instance/database/DDL/backup/instance-config tasks
  - Cloud SQL instance/database/flags/tiers/operations tasks
- **Differentiated benchmark**
  Measure `dcx` features that `bq` and Spanner CLI do not really have:
  - `meta commands`
  - `meta describe`
  - structured dry-run
  - profile validation and testing
  - MCP bridge
  - agent-oriented error envelopes and exit semantics

Do not combine these into one raw score without labeling the distinction.

### 2.2 Benchmark tasks, not just commands

Each benchmark case should represent a user or agent goal:

- list datasets
- inspect table schema
- run a query safely
- fetch database DDL
- recover from a malformed command

This matches the direction of current agent benchmark work, which emphasizes
task completion and fail-to-pass behavior over microbenchmarks alone.

### 2.3 Keep the benchmark reproducible

Every run should pin:

- `dcx` git SHA
- `gcloud` version
- `bq` version
- project and region
- auth mode
- benchmark dataset/database names
- machine type and OS

## 3. Reference Baselines

Use the official CLIs and docs as the comparison baseline:

- BigQuery CLI reference:
  https://docs.cloud.google.com/bigquery/docs/reference/bq-cli-reference
- BigQuery dry run and query behavior:
  https://cloud.google.com/bigquery/docs/running-queries
- BigQuery public datasets:
  https://docs.cloud.google.com/bigquery/public-data
- BigQuery jobs telemetry:
  https://cloud.google.com/bigquery/docs/information-schema-jobs
  https://cloud.google.com/bigquery/docs/information-schema-jobs-by-user
  https://docs.cloud.google.com/bigquery/docs/information-schema-jobs-timeline
- Spanner CLI reference:
  https://docs.cloud.google.com/sdk/gcloud/reference/spanner/cli
- Spanner SQL execution:
  https://docs.cloud.google.com/sdk/gcloud/reference/spanner/databases/execute-sql
- Spanner DDL describe:
  https://cloud.google.com/sdk/gcloud/reference/alpha/spanner/databases/ddl/describe
- Spanner sample schema:
  https://cloud.google.com/spanner/docs/getting-started/gcloud
- Spanner query statistics:
  https://cloud.google.com/spanner/docs/introspection/query-statistics

Background references for agent-native benchmark design:

- Terminal-Bench:
  https://huggingface.co/papers/2601.11868
- LongCLI-Bench:
  https://huggingface.co/papers/2602.14337
- SkillsBench:
  benchmark style to adapt for paired skill/no-skill evaluation
- MCPAgentBench:
  benchmark style to adapt for tool discrimination and token efficiency
- Berkeley BFCL:
  benchmark style to adapt for structured tool-call accuracy

## 4. Systems Under Test

### 4.1 `dcx`

Use the current built binary or pinned release artifact.

Primary surfaces to benchmark:

- BigQuery:
  - `datasets list|get`
  - `tables list|get`
  - `jobs query`
- Spanner:
  - `spanner instances list`
  - `spanner databases list|get`
  - `spanner schema describe`
- Agent contract:
  - `meta commands`
  - `meta describe`
  - `--dry-run`
  - `auth check`
  - `profiles test`
  - `mcp serve`

### 4.2 Baseline CLIs

- BigQuery:
  - `bq`
- Spanner:
  - `gcloud spanner ...`
  - `gcloud alpha spanner databases ddl describe`
  - `gcloud spanner databases execute-sql`

## 5. Dataset and Database Plan

Use four benchmark tiers.

### 5.1 BigQuery tiers

#### Tier A: small public dataset

Use:

- `bigquery-public-data.samples.shakespeare`

Purpose:

- schema fetch
- simple list/get
- low-cost count/aggregate query

#### Tier B: medium public dataset

Use:

- `bigquery-public-data.usa_names.usa_1910_2013`

Purpose:

- aggregate queries
- filtered queries
- query planner and dry-run validation

#### Tier C: nested/semi-structured public dataset

Use:

- `bigquery-public-data.github_repos.commits`

Purpose:

- nested field access
- schema inspection depth
- JSON/array-heavy responses

#### Tier D: private benchmark dataset

Create one project-local dataset, for example `dcx_benchmark`, with:

- one narrow table
- one wide table
- one nested/repeated table
- one partitioned and clustered table

Purpose:

- reproducible query costs
- stable schema evolution tests
- controlled malformed-query scenarios

### 5.2 Spanner tiers

#### Tier A: small sample database

Use the official `Singers` / `Albums` schema from the Spanner getting-started
guide.

Purpose:

- instance/database listing
- simple SQL
- DDL describe

#### Tier B: medium scaled sample

Use a deterministic seeded size, for example:

- `10,000` singers
- `50,000` albums

Purpose:

- filtered reads
- moderate result sets
- statement statistics

#### Tier C: large scaled sample

Use a larger deterministic seeded size, for example:

- `100,000` singers
- `500,000` albums

Purpose:

- pagination behavior
- larger SQL outputs
- query stats collection

#### Tier D: private realistic database

Optional, but useful if a stable internal schema exists. Keep it read-only for
the benchmark.

## 6. Task Catalog

Start with 24 to 30 tasks. Expand only after the harness is stable.

### 6.1 BigQuery parity tasks

1. dataset list
2. dataset get
3. table list
4. table get schema
5. dry-run aggregate query
6. execute aggregate query
7. execute nested-field query
8. malformed query error handling
9. auth failure handling
10. not-found handling
11. permission denied handling
12. invalid flag or invalid argument handling

### 6.2 Spanner parity tasks

1. instance list
2. database list
3. database get
4. DDL describe
5. baseline-only simple `SELECT` via `gcloud spanner databases execute-sql`
6. baseline-only filtered `SELECT` via `gcloud spanner databases execute-sql`
7. bad database error handling
8. bad SQL error handling
9. auth failure handling
10. not-found handling
11. permission denied handling

`dcx` does not currently expose a Spanner query execution command, so the SQL
tasks above should be marked `N/A` for `dcx` in the parity scorecard unless a
future `dcx` Spanner query surface is added.

### 6.3 `dcx` differentiated tasks

1. discover commands with `meta commands`
2. inspect one command contract with `meta describe`
3. run a structured dry-run for a read command
4. validate auth with `auth check`
5. validate a profile with `profiles test`
6. call one command over MCP
7. run paired discovery with and without SKILL.md augmentation
8. measure token usage for one representative multi-step task

### 6.4 Paired skill evaluation

For every differentiated `dcx` task that relies on discoverability, run two
variants:

- **Vanilla**
  Only command help and standard CLI discovery are available
- **Skill-augmented**
  The same task is run with the relevant `SKILL.md` and generated references

Report a SkillsBench-style normalized gain:

`(pass_skill - pass_vanilla) / (1 - pass_vanilla)`

This is the clearest benchmark for whether thin router skills and generated
references materially improve agent success.

## 7. Benchmark Artifact Layout

Recommended repo structure:

```text
benchmarks/
  README.md
  manifest.yaml
  tasks/
    bigquery_overlap.yaml
    spanner_overlap.yaml
    dcx_differentiated.yaml
  data/
    bigquery/
      seed.sql
      expected/
    spanner/
      schema.sql
      seed.sql
      expected/
  scripts/
    seed_bigquery.sh
    seed_spanner.sh
    run_benchmarks.sh
    collect_bigquery_jobs.sql
    score_results.py
  results/
    raw/
    scorecards/
```

## 8. Task Spec Shape

Each task should be a checked-in YAML object with:

```yaml
id: bq-table-get
track: parity
system: bigquery
dataset_tier: small
goal: Fetch schema for a known table
cli_variants:
  - name: dcx
    command: dcx tables get --project-id {project} --dataset-id {dataset} --table-id {table} --format json
  - name: bq
    command: bq show --schema --format=prettyjson {project}:{dataset}.{table}
validation:
  type: json_keys
  expected_keys: [schema]
metrics:
  - exit_code
  - wall_clock_ms
  - stdout_bytes
  - stderr_bytes
  - token_input_estimate
  - token_output_estimate
```

The runner should resolve placeholders such as `{project}`, `{dataset}`,
`{table}`, `{instance}`, and `{database}` from checked-in bindings in
`benchmarks/manifest.yaml`.

The validation type can vary:

- `exact_json`
- `json_keys`
- `semantic_sql_result`
- `stderr_contains`
- `exit_code_only`

Optional task attributes for agent-oriented runs:

- `skill_mode`: `vanilla` or `skill_augmented`
- `error_class`: `auth`, `permission_denied`, `not_found`, `bad_sql`,
  `invalid_flag`, or `infra`
- `expected_recovery_action`: `retry`, `fix_input`, `reauth`, `stop`, or
  `switch_command`

## 9. Pipeline

### 9.1 Environment freeze

Record once per run:

- benchmark run ID
- git SHA
- CLI versions
- OS and CPU
- region
- project
- auth source

### 9.2 Seed data

Before each benchmark cycle:

- create or reset benchmark datasets and databases
- apply deterministic SQL seed files
- record object names in `benchmarks/manifest.yaml`

### 9.3 Execute benchmark matrix

For each task and CLI:

1. run 3 cold trials
2. run 10 warm trials
3. capture stdout, stderr, exit code, and wall-clock time
4. run task-specific validation
5. record retries if the harness had to re-run
6. record token estimates for prompts, tool definitions, help text, stdout,
   and stderr when the task is executed through an agent harness

Command templates in the task specs are rendered with the environment-specific
bindings from `benchmarks/manifest.yaml` before execution.

### 9.4 Collect cloud-side telemetry

For BigQuery tasks, query:

- `INFORMATION_SCHEMA.JOBS`
- `INFORMATION_SCHEMA.JOBS_BY_USER`
- `INFORMATION_SCHEMA.JOBS_TIMELINE`

Capture:

- bytes processed
- cache hit
- total slot milliseconds
- total job duration

For Spanner tasks, capture:

- query execution stats when available
- profile or stats mode outputs
- statement latency summaries from system tables if enabled

### 9.5 Produce scorecards

Emit:

- raw result rows
- per-task summary
- per-track summary
- overall weighted summary
- paired skill/no-skill summaries
- token-efficiency summaries

## 10. Metrics

### 10.1 Correctness

- exact success rate
- semantic success rate
- fail-to-pass rate
- pass-to-pass stability

### 10.2 Efficiency

- p50 wall-clock
- p95 wall-clock
- stdout bytes
- stderr bytes
- commands per task
- retries per task
- startup latency
- input token estimate
- output token estimate
- total token estimate per successful task
- BigQuery bytes processed
- Spanner execution stats where available

### 10.3 Agent usability

- JSON parse success rate
- stable-schema success rate
- help-to-success rate
- required-flag burden
- dry-run fidelity
- contract discoverability
- pagination consistency
- exit-code precision
- error classification accuracy
- recovery-action accuracy
- skill normalized gain

### 10.4 Operational quality

- auth/preflight reliability
- deterministic stderr/stdout separation
- malformed-input handling
- missing-resource handling
- retryability classification

## 11. Process Rules

To keep the benchmark fair:

- use identical auth context where possible
- prefer read-only tasks
- do not compare `dcx` differentiated features inside the parity score
- do not compare raw query latency across different SQL statements
- do not treat one CLI's human-friendly text output as equivalent to another
  CLI's machine-readable JSON unless the validator explicitly allows it

## 12. Scoring Model

Use three top-level scores.

### 12.1 Parity score

Only overlapping BigQuery and Spanner tasks.

Suggested weighting:

- 50% correctness
- 30% efficiency
- 20% operational quality

### 12.2 Agent score

Only machine-usable and agent-facing tasks.

Suggested weighting:

- 35% JSON parse success
- 20% exit-code precision
- 10% error classification accuracy
- 15% discovery and contract usability
- 15% dry-run fidelity
- 10% pagination consistency
- 5% skill normalized gain

### 12.3 Workflow score

Multi-step tasks where command count and retries matter.

Suggested weighting:

- 40% task completion
- 30% command count
- 20% retries
- 10% elapsed time

If one aggregate score is required for an executive summary, use:

- 50% parity
- 30% agent
- 20% workflow

But keep the three component scores visible. A single score hides too much.

## 13. Recommended First Milestone

Implement a minimal but credible v1 benchmark:

1. 8 BigQuery parity tasks
2. 6 Spanner parity tasks
3. 4 of the 8 `dcx` differentiated tasks
4. local runner script
5. checked-in result schema
6. markdown scorecard generator

That is enough to establish baseline numbers without overbuilding the harness.

If the first milestone needs to stay smaller, prioritize:

1. 8 BigQuery parity tasks
2. 4 Spanner parity tasks
3. 4 `dcx` differentiated tasks
4. 4 deliberate error-injection tasks
5. paired vanilla vs skill-augmented runs for 2 representative `dcx` tasks
6. token accounting on those same 2 representative tasks

## 14. Success Criteria

This benchmark is ready to use when:

- tasks are fully reproducible from checked-in manifests
- every task has deterministic validation
- cold and warm runs are separated
- cloud-side telemetry is captured for BigQuery and Spanner tasks
- parity and differentiated scores are reported separately
- raw outputs are preserved for auditability
- paired skill/no-skill results are reported for at least one `dcx` workflow
- error classification accuracy is measured on deliberate failure tasks
- token-efficiency reporting is available for agent-driven runs

## 15. What This Benchmark Should Prove

For `dcx`, the most important claims are not:

- "queries are faster than BigQuery"
- "Spanner is faster through `dcx`"

The benchmark should instead prove:

- `dcx` is competitive on overlapping read tasks
- `dcx` is more predictable for agents
- `dcx` reduces command count and parsing burden
- `dcx` exposes differentiated workflows that existing CLIs do not package well

## 16. Future Extensions

After the deterministic benchmark is stable, optional future extensions:

- Claude Code or Gemini CLI task-run benchmarks on top of the same task catalog
- token-efficiency measurement for CLI output vs MCP tool schema overhead
- mutation-safe workflow benchmarks using confirmation envelopes
- larger cross-source workflows involving profiles, analytics, and MCP
- additional parity tracks for AlloyDB and Cloud SQL once their overlap task
  sets are defined cleanly against baseline CLIs
