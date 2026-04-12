# ca ask Command Reference

## Usage

```bash
dcx ca ask "<question>" [--agent=<AGENT>] [--tables=<TABLE_REFS>] [--profile=<PROFILE>]
  [--format json|text|table]
```

## Flags

| Flag | Required | Description |
|------|----------|-------------|
| `<question>` | Yes | Natural language question (positional) |
| `--agent` | No | Data agent name (BigQuery) |
| `--tables` | No | Comma-separated table refs for ad-hoc context (BigQuery) |
| `--profile` | No | Source profile YAML file |
| `--format` | No | Output: `json` (default), `text`, `table` |

## Response structure (JSON)

- `question` — the original question
- `sql` — the generated SQL query
- `results` — query result rows
- `explanation` — natural language explanation
- `agent` — agent used (BigQuery only)

## Per-source examples

```bash
# BigQuery with agent
dcx ca ask "top errors for support_bot yesterday?" --agent=agent-analytics

# BigQuery with inline tables
dcx ca ask "how many sessions yesterday?" --tables=myproject.analytics.agent_events

# Looker
dcx ca ask --profile sales-looker.yaml "top selling products last month"

# Spanner
dcx ca ask --profile finance-spanner.yaml "total revenue by region"

# AlloyDB
dcx ca ask --profile ops-alloydb.yaml "show all tables"

# Cloud SQL
dcx ca ask --profile app-cloudsql.yaml "active users today"
```

## Notes

- Read-only command — safe to run without confirmation
- Pipe `--format json` to `jq` for scripted analysis
- Questions must not be empty
