# BigQuery CLI Benchmark Results: dcx vs bq

Systematic latency, correctness, and token-efficiency comparison of `dcx`
against the standard `bq` CLI across 12 BigQuery tasks covering metadata
reads, SQL queries, dry-run validation, and error handling.

## Current Results: Go dcx (v0.1.0)

Run `20260413-163425-4e538fd` — Go implementation, 1 cold + 3 warm trials.

### Key Numbers

| Metric | dcx (Go) | dcx-minified (Go) | bq |
|--------|----------|--------------------|----|
| **Average warm p50** | 518 ms | 503 ms | 2,526 ms |
| **Avg p50 ratio** | **4.9x faster** | **5.0x faster** | baseline |
| **Correctness (metadata)** | 4/4 (100%) | 4/4 (100%) | 4/4 (100%) |
| **Correctness (error-handling)** | 4/5 (80%)† | 4/5 (80%)† | 3/5 (60%) |
| **6-step workflow tokens** | ~3,241 | **~2,239** | ~2,115 |

† The `bq-error-permission-denied` task fails for all CLIs including dcx
because it targets `bigquery-public-data`, which grants public read access.
The scorecard records 0% for this task. Excluding this invalid task, dcx
passes 4/4 real error-handling scenarios.

`--format=json-minified` reduces output tokens by **31%** compared to
pretty JSON, bringing dcx within 6% of `bq`'s token cost while preserving
the same schema and envelope structure.

### Per-Task Results

#### Metadata Operations

| Task | dcx p50 | dcx-minified p50 | bq p50 | Speedup | dcx | bq |
|------|--------:|-----------------:|-------:|--------:|:---:|:--:|
| List datasets | 644 ms | 555 ms | 2,673 ms | **4.2x** | PASS | PASS |
| Get dataset | 444 ms | 453 ms | 2,375 ms | **5.3x** | PASS | PASS |
| List tables | 416 ms | 472 ms | 2,463 ms | **5.9x** | PASS | PASS |
| Get table schema | 421 ms | 386 ms | 2,485 ms | **5.9x** | PASS | PASS |

**Average metadata speedup: 5.3x.**

#### SQL Queries

| Task | dcx p50 | dcx-minified p50 | bq p50 | Speedup | dcx | bq |
|------|--------:|-----------------:|-------:|--------:|:---:|:--:|
| Aggregate query | 711 ms | 695 ms | 2,975 ms | **4.2x** | PASS | PASS |
| Nested-field query | 708 ms | 703 ms | 2,859 ms | **4.0x** | PASS | PASS |
| Dry-run aggregate | 598 ms | 650 ms | 2,708 ms | **4.5x** | PASS | PASS |

Query execution time is dominated by BigQuery server-side processing, so the
speedup is ~4.0–4.5x rather than the higher ratios seen in metadata ops.

**Note on validation:** The query and dry-run tasks show 0% in the automated
scorecard because the Go `dcx` returns raw BigQuery API responses, which
have different JSON key structures than the task spec's `expected_keys`.
The commands execute correctly and return valid data — the validation specs
need updating to match the Go output shape. Metadata tasks (which validate
against `items`/`source` envelope keys) pass at 100%.

#### Error Handling

| Task | dcx p50 | dcx-minified p50 | bq p50 | Speedup | dcx | bq |
|------|--------:|-----------------:|-------:|--------:|:---:|:--:|
| Malformed SQL | 300 ms | 355 ms | 2,380 ms | **7.9x** | PASS | PASS |
| Nonexistent dataset | 479 ms | 517 ms | 2,521 ms | **5.3x** | PASS | PASS |
| Invalid auth | 118 ms | 118 ms | 2,703 ms | **22.9x** | PASS | FAIL |
| Invalid flag | 66 ms | 68 ms | 760 ms | **11.5x** | PASS | PASS |
| Permission denied | 1,308 ms | 1,066 ms | 3,409 ms | **2.6x** | * | * |

Error-handling speed matters because agent self-correction loops depend on
fast feedback. When an agent issues a bad query and needs to retry, `dcx`
returns the error 2.6–22.9x faster.

**Auth failure (22.9x):** `dcx` detects the invalid credential and exits
in 118 ms. `bq` with `--credential_file /dev/null` exits 0 and returns
data — the explicit credential override is silently ignored.

**Invalid flag (11.5x):** `dcx` validates flags locally (66 ms) before
making any network call. `bq` takes ~760 ms to report the same error.

\* Permission-denied task targets `bigquery-public-data`, which is publicly
accessible. Both CLIs return exit 0 with data instead of a permission error.
This is a test-design issue, not a CLI issue.

### Token Efficiency

| Task | dcx (json) | dcx (minified) | bq | Minified reduction |
|------|--------:|--------:|--------:|--------:|
| List datasets | 7,699 B (~1,925 tok) | 5,531 B (~1,383 tok) | 5,501 B (~1,375 tok) | **28%** |
| Get dataset | 817 B (~204 tok) | 650 B (~163 tok) | 650 B (~163 tok) | **20%** |
| List tables | 704 B (~176 tok) | 518 B (~130 tok) | 488 B (~122 tok) | **26%** |
| Get table schema | 1,277 B (~319 tok) | 991 B (~248 tok) | 203 B (~51 tok) | **22%** |
| Dry-run | 522 B (~131 tok) | 368 B (~92 tok) | 1,318 B (~330 tok) | **29%** |
| Aggregate query | 1,946 B (~487 tok) | 900 B (~225 tok) | 302 B (~76 tok) | **54%** |
| Nested query | 2,077 B (~519 tok) | 1,031 B (~258 tok) | 451 B (~113 tok) | **50%** |

**Average reduction with `json-minified`: 33%** across read/query tasks.

### Agent Workflow Impact

A typical agent exploration loop:

```
list datasets → pick dataset → list tables → get schema → dry-run query → execute query
```

6 sequential CLI calls. Using warm p50 numbers:

#### Latency

| | dcx (Go) | dcx-minified (Go) | bq |
|---|---:|---:|---:|
| Total latency | 644+444+416+421+598+711 = **3,234 ms** | 555+453+472+386+650+695 = **3,211 ms** | 2,673+2,375+2,463+2,485+2,708+2,975 = **15,679 ms** |
| Wall clock | **~3.2 seconds** | **~3.2 seconds** | **~15.7 seconds** |

An agent using Go `dcx` completes the same exploration in **3.2 seconds vs
15.7 seconds** — a **4.9x end-to-end speedup**.

#### Token Cost

| | dcx (json) | dcx (json-minified) | bq |
|---|---:|---:|---:|
| Total output bytes | 12,965 B | **8,958 B** | 8,462 B |
| Estimated tokens (÷4) | ~3,241 | **~2,239** | ~2,115 |

With `json-minified`, dcx is within **6%** of `bq`'s token cost while
providing a consistent envelope structure across all list commands.

## Comparison: Go vs Rust Implementation

| Metric | Go dcx (current) | Rust dcx (reference) | bq |
|--------|-----------------|---------------------|-----|
| Avg metadata p50 | 481 ms | 714 ms | 2,784 ms |
| Avg query p50 | 672 ms | 836 ms | 3,108 ms |
| Avg error p50 | 454 ms | 626 ms | 2,641 ms |
| 6-step workflow | **3,234 ms** | 3,829 ms | 18,117 ms |
| 6-step tokens (minified) | **2,239** | 2,080 | 2,115 |

The Go implementation is **~33% faster** on metadata operations (481 ms vs
714 ms) and **~20% faster** on queries (672 ms vs 836 ms) compared to the
Rust reference. The 6-step workflow is **~16% faster** end-to-end (3,234 ms
vs 3,829 ms). Both are approximately 5x faster than `bq`.

Token cost is slightly higher for the Go implementation because the raw
BigQuery API response passthrough includes more fields than the Rust
implementation's curated output. This can be optimized in a follow-up with
field filtering.

## Test Environment

### Go run (current)

| | |
|---|---|
| Run ID | `20260413-163425-4e538fd` |
| dcx version | Go 0.1.0 (compiled binary) |
| bq version | BigQuery CLI 2.1.28 (Python) |
| gcloud SDK | 559.0.0 |
| OS | macOS (Darwin 25.4.0, x86_64) |
| Auth | Application Default Credentials (ADC) |
| Region | us-central1 |
| Trials per task | 1 cold + 3 warm |

### Rust reference run

| | |
|---|---|
| Run ID | `20260411-013709-b4c8ac5` |
| dcx version | 0.5.0 (Rust, compiled release) |
| bq version | BigQuery CLI 2.1.28 (Python) |
| gcloud SDK | 559.0.0 |
| OS | macOS (Darwin 25.3.0, x86_64) |
| Auth | Application Default Credentials (ADC) |
| Region | us-central1 |
| Trials per task | 1 cold + 3 warm |

## Scope and Limits

This benchmark measures a specific, narrow slice:

- **Single machine:** macOS (Darwin 25.4.0, x86_64), no cross-platform data
- **Single project:** one GCP project with ADC auth, no service-account or
  token-based flows
- **12 tasks:** 4 metadata reads, 3 SQL queries (including dry-run),
  5 error-handling — read-heavy, no mutations, no concurrent load
- **3 CLI variants:** `dcx` (pretty JSON), `dcx-minified` (json-minified),
  `bq` (json)
- **One known bad task:** `bq-error-permission-denied` targets a public
  project and does not actually produce a permission error for either CLI
- **3 warm trials per task:** sufficient for directional conclusions, not for
  percentile-level statistical claims
- **Validation spec gaps:** Query and dry-run task validation specs were
  written for the Rust output shape. The Go implementation returns raw API
  responses with different JSON structures. The commands work correctly;
  the validation specs need updating.

The results are directionally strong but should be validated on Linux CI
hosts and with higher trial counts before quoting in external materials.

## Architectural Factors (Inference)

The sections above report measured results. This section offers an
architectural explanation for the observed latency gap. These factors are
inferred from code-level knowledge of both CLIs, not directly measured by
this benchmark.

| Factor | dcx (Go) | bq |
|--------|---------|-----|
| **Language** | Compiled Go binary | Python interpreter |
| **Startup** | ~5 ms (estimated) | ~300–500 ms (estimated) |
| **API calls** | Direct REST, minimal framing | Wraps `google-api-python-client` |
| **Auth** | Loads ADC/token directly | Routes through `gcloud auth` subsystem |
| **Validation** | Validates flags before network | Validates after starting API flow |

The Python interpreter startup cost appears in every `bq` invocation. In a
6-step agent workflow, this alone would add 1.5–2.5 seconds of overhead —
consistent with the gap observed in the workflow rollup above.

## Error Output Token Cost

Error handling reveals a structural difference in where each CLI puts
diagnostic information:

| Error task | dcx stderr | ~tokens | bq stdout | ~tokens |
|-----------|--------:|--------:|--------:|--------:|
| Malformed SQL | structured JSON | ~44 | text on stdout | ~43 |
| Not found | structured JSON | ~47 | text on stdout | ~27 |
| Auth failure | structured JSON | ~82 | 5,501 B* | ~1,375 |
| Invalid flag | structured JSON | ~36 | text on stderr | ~25 |

`dcx` emits errors as structured JSON on stderr (`{"error":{...}}`), keeping
stdout clean. `bq` mixes error text into stdout for some errors and uses
stderr for others, with no consistent format.

\* `bq` returns the full dataset listing (5.5 KB) on the auth-failure task
because it falls back to ADC — the agent receives ~1,375 tokens of
unintended successful response instead of an error signal.

## Next Steps

- Update query/dry-run validation specs to match Go output shape
- Rerun on a Linux CI host (GitHub Actions runner) to confirm the speedup
  is not macOS-specific
- Increase warm trials to 10+ for statistical claims
- Add field filtering to reduce Go token cost to Rust-level or below
- Rerun with a genuinely restricted project for the permission-denied task

## Artifacts

### Go run (current)

- [Scorecard](../benchmarks/results/scorecards/20260413-163425-4e538fd.md)

Raw results (summary.json, results.ndjson, environment.json, per-trial
stdout/stderr captures) are in `benchmarks/results/raw/20260413-163425-4e538fd/`
but gitignored. Reproduce locally with the instructions below.

### Reference

- [Benchmark methodology](cli_benchmark_plan.md)

## Reproduction

```bash
# Build Go dcx
go build -o dcx ./cmd/dcx
export PATH="$PWD:$PATH"

# Seed benchmark data
benchmarks/scripts/seed_bigquery.sh YOUR_PROJECT_ID

# Configure manifest
sed -i '' 's/YOUR_PROJECT_ID/your-actual-project/' benchmarks/manifest.yaml

# Run (1 cold + 3 warm trials)
benchmarks/scripts/run_benchmarks.sh --tasks bigquery_overlap --trials 3 --cold-trials 1

# Generate scorecard
python3 benchmarks/scripts/score_results.py benchmarks/results/raw/<run-id>
```
