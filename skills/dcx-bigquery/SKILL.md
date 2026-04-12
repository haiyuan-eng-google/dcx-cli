---
name: dcx-bigquery
description: BigQuery router skill — authentication, global flags, SQL queries, schema inspection, and connections. Entry point for all BigQuery-specific dcx usage.
---

## When to use this skill

Use when the user wants to:
- Set up dcx authentication or understand global flags
- Run SQL queries against BigQuery
- Inspect table or view schemas
- Discover BigQuery connections
- Understand dcx output formats

## Authentication

dcx resolves credentials in order (first match wins):

1. `--token` flag or `DCX_TOKEN` env var
2. `--credentials-file` flag or `DCX_CREDENTIALS_FILE` env var
3. Stored credentials from `dcx auth login` (OAuth browser flow)
4. `GOOGLE_APPLICATION_CREDENTIALS` env var
5. Default ADC (`gcloud auth application-default login` or GCE metadata)

```bash
dcx auth login      # opens browser for Google OAuth
dcx auth status     # shows active credential source
dcx auth logout     # clears stored credentials
```

## Global flags

| Flag | Env var | Default | Required |
|------|---------|---------|----------|
| `--project-id` | `DCX_PROJECT` | — | Yes (all commands) |
| `--dataset-id` | `DCX_DATASET` | — | Analytics commands only |
| `--location` | `DCX_LOCATION` | `US` | No |
| `--format` | — | `json` | No |
| `--token` | `DCX_TOKEN` | — | No |
| `--credentials-file` | `DCX_CREDENTIALS_FILE` | — | No |

## Output formats

| Format | Best for |
|--------|----------|
| `--format json` | Automation, piping to `jq`, CI |
| `--format table` | Visual scanning in terminal |
| `--format text` | Demos, human-readable summaries |

## SQL queries

```bash
dcx jobs query --project-id my-proj --query "SELECT COUNT(*) FROM \`my-proj.ds.table\`" --format table
dcx jobs query --project-id my-proj --query "SELECT 1" --dry-run
```

- `--dry-run` shows the API request without executing
- `--use-legacy-sql` enables legacy SQL dialect (default: standard SQL)
- Does **not** require `--dataset-id`

## Schema inspection

```bash
# Table metadata (includes schema.fields)
dcx tables get --project-id P --dataset-id D --table-id T --format json

# INFORMATION_SCHEMA query
dcx jobs query --project-id P \
  --query "SELECT column_name, data_type FROM \`P.D\`.INFORMATION_SCHEMA.COLUMNS WHERE table_name = 'T'" \
  --format table
```

## Connections

BigQuery connections are inspected via INFORMATION_SCHEMA queries:

```bash
dcx jobs query --project-id P \
  --query "SELECT routine_name, remote_function_info FROM \`P.D\`.INFORMATION_SCHEMA.ROUTINES WHERE remote_function_info IS NOT NULL" \
  --format table
```

## Decision rules

- Use `dcx jobs query` for direct SQL execution
- Use `dcx tables get` for single-table schema inspection
- Use INFORMATION_SCHEMA for cross-table schema queries
- Use `--dry-run` to verify without executing
- `jobs query` does not require `--dataset-id`; analytics commands do

## Constraints

- TIMESTAMP values are auto-converted from epoch to ISO 8601
- Long-running queries are polled automatically
- Connection management (create/delete) requires BigQuery Connection API directly

## References

- See **dcx-bigquery-api** for Discovery-generated dataset/table/routine/model commands
- See **dcx-analytics** for agent analytics workflows
- See **dcx-ca** for natural language queries
