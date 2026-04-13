# dcx CLI Benchmark Suite

Systematic benchmark comparing `dcx` against `bq` and `gcloud spanner` CLIs.

> **Carried over from [bqx-cli](https://github.com/haiyuan-eng-google/bqx-cli).**
> Task specs, runner, and scorer are ready to use. The benchmark suite
> becomes runnable once the Go command surface is implemented. Reference
> results from the Rust implementation are in
> [docs/benchmark_results_bigquery.md](../docs/benchmark_results_bigquery.md).

See [docs/cli_benchmark_plan.md](../docs/cli_benchmark_plan.md) for the full
design rationale, scoring model, and success criteria.

## Quick Start

### 1. Configure

Edit `manifest.yaml` with your project ID and resource names:

```bash
$EDITOR manifest.yaml
```

### 2. Seed Data

```bash
# BigQuery (creates dcx_benchmark dataset + tables)
./scripts/seed_bigquery.sh YOUR_PROJECT_ID

# Spanner (creates instance + database + sample data)
./scripts/seed_spanner.sh YOUR_PROJECT_ID small

# Profile for dcx-profiles-test task (optional but recommended)
# Create a BigQuery profile YAML file manually:
mkdir -p ~/.config/dcx/profiles
cat > ~/.config/dcx/profiles/bench.yaml <<'PROF'
name: bench
source_type: bigquery
project: YOUR_PROJECT_ID
location: US
PROF
# Verify the profile is discoverable:
dcx profiles validate --profile bench
```

### 3. Run Benchmarks

```bash
# Run all tasks (3 cold + 10 warm trials per CLI variant)
./scripts/run_benchmarks.sh

# Run a specific track
./scripts/run_benchmarks.sh --tasks bigquery_overlap

# Adjust trial count
./scripts/run_benchmarks.sh --trials 5 --cold-trials 1
```

### 4. Generate Scorecards

```bash
python3 scripts/score_results.py results/raw/<run-id>
```

## Directory Layout

```
benchmarks/
  manifest.yaml              # Environment bindings ({project}, {dataset}, etc.)
  tasks/
    bigquery_overlap.yaml     # 12 BigQuery parity tasks
    spanner_overlap.yaml      # 11 Spanner parity tasks
    dcx_differentiated.yaml   #  8 dcx-only differentiated tasks
  data/
    bigquery/seed.sql         # BigQuery seed data (Tier D private tables)
    spanner/schema.sql        # Spanner DDL (Singers/Albums)
    spanner/seed.sql          # Spanner seed data (Tier A small)
  scripts/
    seed_bigquery.sh          # Create and populate BigQuery benchmark dataset
    seed_spanner.sh           # Create and populate Spanner benchmark instance
    run_benchmarks.sh         # Execute benchmark matrix
    collect_bigquery_jobs.sql # Post-run BigQuery telemetry collection
    score_results.py          # Generate summary JSON + markdown scorecards
  results/
    raw/                      # Per-run NDJSON results + stdout/stderr captures
    scorecards/               # Generated markdown scorecards
```

## Dependencies

- `dcx` (built from this repo)
- `bq` (BigQuery CLI, part of gcloud SDK)
- `gcloud` (Google Cloud SDK)
- `yq` (YAML processor, for task spec parsing)
- `python3` (for scorecard generation)

## Task Tracks

| Track | Tasks | CLIs Compared |
|-------|-------|---------------|
| BigQuery parity | 12 | dcx vs bq |
| Spanner parity | 11 | dcx vs gcloud spanner |
| dcx differentiated | 8 | dcx only |

See the task YAML files for full specifications.
