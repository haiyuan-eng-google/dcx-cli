---
name: dcx-ca
description: Conversational Analytics — natural language queries over BigQuery, Looker, AlloyDB, Spanner, and Cloud SQL, plus data agent management and verified queries.
---

## When to use this skill

Use when the user wants to:
- Ask natural language questions over any Data Cloud source
- Create or list data agents (BigQuery)
- Add verified queries to improve agent accuracy
- Set up CA profiles for Looker or database sources
- Understand how CA routes across source types

## Prerequisites

- BigQuery: `--project-id` (or `DCX_PROJECT`)
- All other sources: `--profile` (YAML file defining source connection)
- `ca ask` uses `--location` (defaults to `US`) for chat/queryData endpoints
- Agent management (`create-agent`, `list-agents`, `add-verified-query`) always uses `locations/global` — the `--location` flag is ignored for these commands
- Does **not** require `--dataset-id`

See **dcx-bigquery** for authentication.

## Command routing

| User goal | Command |
|-----------|---------|
| Ask a natural language question | `dcx ca ask "<question>" [--agent\|--tables\|--profile]` |
| Create a data agent | `dcx ca create-agent --name=NAME --tables=REFS` |
| List data agents | `dcx ca list-agents` |
| Add a verified query | `dcx ca add-verified-query --agent=NAME --question=Q --query=SQL` |

## Source routing

The `--profile` source_type determines which API is called automatically:

| Source | API Family | Access method |
|--------|-----------|---------------|
| BigQuery | Chat/DataAgent | `--agent`, `--tables`, or `--profile` |
| Looker | Chat/DataAgent | `--profile` only |
| Looker Studio | Chat/DataAgent | `--profile` only |
| AlloyDB | QueryData | `--profile` only |
| Spanner | QueryData | `--profile` only |
| Cloud SQL | QueryData | `--profile` only |

## Workflows

### BigQuery (agents + inline tables)

```bash
dcx ca create-agent --name=agent-analytics \
  --tables=myproject.analytics.agent_events \
  --instructions="You help analyze AI agent performance."

dcx ca ask "What is the error rate for support_bot?" --agent=agent-analytics
```

### Profile-based (Looker, databases)

```bash
dcx ca ask --profile sales-looker.yaml "top selling products"
dcx ca ask --profile finance-spanner.yaml "revenue by region"
dcx ca ask --profile ops-alloydb.yaml "show all tables"
dcx ca ask --profile app-cloudsql.yaml "active users today"
```

## Decision rules

- Use `--agent` for pre-configured BigQuery data agents
- Use `--tables` for ad-hoc BigQuery queries without an agent
- Use `--profile` for Looker, AlloyDB, Spanner, Cloud SQL, or BigQuery profiles
- `--profile` cannot be combined with `--agent` or `--tables`
- `--agent` and `--tables` are mutually exclusive
- `--format text` for interactive exploration; `--format json` for scripts

## Constraints

- CA API is currently in preview
- Data agents are project-scoped, BigQuery-only, and live at `locations/global`
- Database sources (AlloyDB, Spanner, Cloud SQL) do not support data agents or visualizations
- Looker profiles work with `ca ask` but not `ca create-agent`

## References

- `references/ask.md` — ca ask flags, output structure, per-source examples
- `references/create-agent.md` — agent creation, views, verified queries format
- `references/looker.md` — Looker profile setup, OAuth, explore format
- `references/querydata.md` — database CA setup, prerequisites, context sets
