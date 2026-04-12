#!/usr/bin/env python3
"""Generate benchmark scorecards from raw results.

Usage:
    python3 score_results.py <results_dir>

Reads results.ndjson from the given run directory and produces:
  - per-task summary (CSV + markdown)
  - per-track summary
  - overall weighted scores
"""

import json
import os
import statistics
import sys
from collections import defaultdict
from pathlib import Path


def load_results(results_dir: Path) -> list[dict]:
    """Load NDJSON results file."""
    results_file = results_dir / "results.ndjson"
    if not results_file.exists():
        print(f"ERROR: {results_file} not found", file=sys.stderr)
        sys.exit(1)

    results = []
    with open(results_file) as f:
        for line in f:
            line = line.strip()
            if line:
                results.append(json.loads(line))
    return results


def summarize_task(task_id: str, cli: str, trials: list[dict]) -> dict:
    """Compute summary statistics for one task+cli combination."""
    warm_trials = [t for t in trials if t["trial_type"] == "warm"]
    cold_trials = [t for t in trials if t["trial_type"] == "cold"]

    def stats(trial_list, key):
        values = [t[key] for t in trial_list]
        if not values:
            return {"count": 0}
        return {
            "count": len(values),
            "p50": statistics.median(values),
            "p95": sorted(values)[int(len(values) * 0.95)] if len(values) >= 2 else values[0],
            "mean": statistics.mean(values),
            "min": min(values),
            "max": max(values),
        }

    # Use the validation result if present; fall back to exit_code == 0.
    success_count = sum(
        1 for t in warm_trials
        if t.get("validation", "pass" if t["exit_code"] == 0 else "fail") == "pass"
    )
    total = len(warm_trials)

    return {
        "task_id": task_id,
        "cli": cli,
        "success_rate": success_count / total if total > 0 else 0,
        "warm_wall_clock": stats(warm_trials, "wall_clock_ms"),
        "cold_wall_clock": stats(cold_trials, "wall_clock_ms"),
        "warm_stdout_bytes": stats(warm_trials, "stdout_bytes"),
        "warm_stderr_bytes": stats(warm_trials, "stderr_bytes"),
    }


def generate_markdown(summaries: list[dict], results_dir: Path) -> str:
    """Generate a markdown scorecard."""
    lines = []
    lines.append("# Benchmark Scorecard")
    lines.append("")

    # Load environment if available.
    env_file = results_dir / "environment.json"
    if env_file.exists():
        with open(env_file) as f:
            env = json.load(f)
        lines.append("## Environment")
        lines.append("")
        lines.append(f"- **Run ID:** {env.get('run_id', 'unknown')}")
        lines.append(f"- **Git SHA:** {env.get('git_sha', 'unknown')}")
        lines.append(f"- **dcx version:** {env.get('dcx_version', 'unknown')}")
        lines.append(f"- **bq version:** {env.get('bq_version', 'unknown')}")
        lines.append(f"- **OS:** {env.get('os', 'unknown')}")
        lines.append(f"- **Project:** {env.get('project', 'unknown')}")
        lines.append("")

    # Per-task table.
    lines.append("## Per-Task Results")
    lines.append("")
    lines.append("| Task | CLI | Success Rate | p50 (ms) | p95 (ms) | Stdout (bytes) |")
    lines.append("|------|-----|-------------|----------|----------|----------------|")

    for s in sorted(summaries, key=lambda x: (x["task_id"], x["cli"])):
        warm = s["warm_wall_clock"]
        stdout = s["warm_stdout_bytes"]
        p50 = f"{warm['p50']:.0f}" if warm.get("p50") is not None else "—"
        p95 = f"{warm['p95']:.0f}" if warm.get("p95") is not None else "—"
        out_p50 = f"{stdout['p50']:.0f}" if stdout.get("p50") is not None else "—"
        lines.append(
            f"| {s['task_id']} | {s['cli']} | {s['success_rate']:.0%} | {p50} | {p95} | {out_p50} |"
        )

    lines.append("")

    # Per-CLI aggregate.
    by_cli = defaultdict(list)
    for s in summaries:
        by_cli[s["cli"]].append(s)

    lines.append("## Per-CLI Summary")
    lines.append("")
    lines.append("| CLI | Tasks | Avg Success | Avg p50 (ms) |")
    lines.append("|-----|-------|-------------|-------------|")

    for cli, cli_summaries in sorted(by_cli.items()):
        avg_success = statistics.mean(s["success_rate"] for s in cli_summaries)
        p50_values = [
            s["warm_wall_clock"]["p50"]
            for s in cli_summaries
            if s["warm_wall_clock"].get("p50") is not None
        ]
        avg_p50 = f"{statistics.mean(p50_values):.0f}" if p50_values else "—"
        lines.append(f"| {cli} | {len(cli_summaries)} | {avg_success:.0%} | {avg_p50} |")

    lines.append("")
    return "\n".join(lines)


def main():
    if len(sys.argv) < 2:
        print("Usage: score_results.py <results_dir>", file=sys.stderr)
        sys.exit(1)

    results_dir = Path(sys.argv[1])
    results = load_results(results_dir)

    if not results:
        print("No results found.", file=sys.stderr)
        sys.exit(1)

    # Group by (task_id, cli).
    groups = defaultdict(list)
    for r in results:
        groups[(r["task_id"], r["cli"])].append(r)

    summaries = []
    for (task_id, cli), trials in groups.items():
        summaries.append(summarize_task(task_id, cli, trials))

    # Write JSON summary.
    summary_file = results_dir / "summary.json"
    with open(summary_file, "w") as f:
        json.dump(summaries, f, indent=2)
    print(f"==> Summary written to {summary_file}")

    # Write markdown scorecard.
    scorecard_md = generate_markdown(summaries, results_dir)
    scorecard_dir = results_dir.parent.parent / "scorecards"
    scorecard_dir.mkdir(parents=True, exist_ok=True)
    scorecard_file = scorecard_dir / f"{results_dir.name}.md"
    with open(scorecard_file, "w") as f:
        f.write(scorecard_md)
    print(f"==> Scorecard written to {scorecard_file}")


if __name__ == "__main__":
    main()
