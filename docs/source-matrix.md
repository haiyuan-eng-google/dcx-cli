# Source Matrix

Cross-source command matrix for dcx v0.5.0.

## Command Coverage

| Command | BigQuery | Spanner | AlloyDB | Cloud SQL | Looker |
|---------|----------|---------|---------|-----------|--------|
| `datasets list\|get` | Yes | — | — | — | — |
| `datasets insert\|delete` | Yes | — | — | — | — |
| `tables list\|get` | Yes | — | — | — | — |
| `tables insert\|delete` | Yes | — | — | — | — |
| `routines list\|get` | Yes | — | — | — | — |
| `models list\|get` | Yes | — | — | — | — |
| `jobs list\|get\|query` | Yes | — | — | — | — |
| `instances list\|get` | — | Yes | Yes | Yes | Yes |
| `clusters list\|get` | — | — | Yes | — | — |
| `databases list\|get` | — | Yes | — | Yes | — |
| `databases insert\|delete` | — | — | — | Yes | — |
| `databases create\|drop-database\|update-ddl` | — | Yes | — | — | — |
| `databases get-ddl` | — | Yes | — | — | — |
| `backups list\|get` | — | Yes | Yes | — | Yes |
| `backups create\|delete` | — | Yes | — | — | — |
| `backupRuns list\|get` | — | — | — | Yes | — |
| `users list\|get` | — | — | Yes | Yes | — |
| `users insert\|delete` | — | — | — | Yes | — |
| `operations list\|get` | — | — | Yes | Yes | — |
| `databaseOperations list` | — | Yes | — | — | — |
| `instanceConfigs list\|get` | — | Yes | — | — | — |
| `flags list` | — | — | — | Yes | — |
| `tiers list` | — | — | — | Yes | — |
| `explores list\|get` | — | — | — | — | Yes |
| `dashboards list\|get` | — | — | — | — | Yes |
| `schema describe` | — | Yes | Yes | Yes | — |
| `databases list` (profile) | — | — | Yes | — | — |
| `profiles list\|show\|validate` | All | All | All | All | All |
| `ca ask` | Yes | Yes | Yes | Yes | Yes |
| `ca create-agent\|list-agents` | Yes | — | — | — | — |
| `ca add-verified-query` | Yes | — | — | — | — |

## Profile Requirements

| Source Type | `source_type` | Required Fields |
|------------|---------------|-----------------|
| BigQuery | `bigquery` | `project` |
| Looker | `looker` | `looker_instance_url`, `looker_explores` |
| Looker Studio | `looker_studio` | `studio_datasource_id` |
| Spanner | `spanner` | `project`, `instance_id`, `database_id` |
| AlloyDB | `alloy_db` | `project`, `cluster_id`, `instance_id`, `database_id` |
| Cloud SQL | `cloud_sql` | `project`, `instance_id`, `database_id`, `db_type` |

## API Families

| Source | CA API Family | SQL Dialect |
|--------|--------------|-------------|
| BigQuery | Chat / DataAgent | GoogleSQL |
| Looker | Chat / DataAgent | LookML |
| Looker Studio | Chat / DataAgent | — |
| Spanner | QueryData | GoogleSQL |
| AlloyDB | QueryData | PostgreSQL |
| Cloud SQL | QueryData | MySQL or PostgreSQL |

## Output Modes

All commands support `--format json|json-minified|table|text`. Default is `json`.

| Format | Best for |
|--------|----------|
| `json` | Automation, `jq` piping, CI pipelines |
| `table` | Visual terminal scanning |
| `text` | Human-readable summaries, demos |

## Discovery Sources

| Service | Discovery Document | Namespace |
|---------|-------------------|-----------|
| BigQuery | `bigquery/v2` | top-level |
| Spanner | `spanner/v1` | `spanner` |
| AlloyDB | `alloydb/v1` | `alloydb` |
| Cloud SQL | `sqladmin/v1` | `cloudsql` |
| Looker (admin) | `looker/v1` | `looker` |

## Known Limitations

- BigQuery datasets/tables, Cloud SQL databases/users, and Spanner databases/backups support write operations; all other source commands are read-only
- Schema describe uses CA QueryData — requires a valid profile
- AlloyDB `--location` defaults to `-` (all locations), not `US`
- Looker content commands use per-instance API, admin commands use GCP API
- CA API is in preview — database source support may evolve
- `ca create-agent` only supports BigQuery
- Cloud SQL `db_type` must be `mysql` or `postgresql`
