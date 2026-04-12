# Database CA Setup (QueryData API)

Database sources (AlloyDB, Spanner, Cloud SQL) use the **QueryData API**,
not the Chat/DataAgent API used by BigQuery and Looker. The CLI handles
routing automatically based on the profile's `source_type`.

## Profiles

### AlloyDB

```yaml
name: ops-alloydb
source_type: alloy_db
project: my-gcp-project
location: us-central1
cluster_id: my-cluster
instance_id: my-primary
database_id: opsdb
```

Prerequisites: AlloyDB API enabled, Data API Access enabled on instance,
IAM database user created.

### Spanner

```yaml
name: finance-spanner
source_type: spanner
project: my-gcp-project
location: us-central1
instance_id: my-spanner-instance
database_id: my-database
```

Prerequisites: Spanner API enabled, `spanner.databases.read` IAM permission.
No Data API toggle needed — simplest database source to set up.

### Cloud SQL

```yaml
name: app-cloudsql
source_type: cloud_sql
project: my-gcp-project
location: us-central1
instance_id: my-app-db
database_id: myapp
db_type: postgresql    # "mysql" or "postgresql"
```

Prerequisites: Data API Access enabled (`gcloud sql instances patch --data-api-access=ALLOW_DATA_API`),
IAM authentication enabled, IAM database user created.

## Optional: context sets

All database profiles support an optional `context_set_id` for pre-authored
context that improves query accuracy:

```yaml
context_set_id: my-context-set
```

When provided, sent as `agentContextReference` in the QueryData request.

## SQL dialects

- Spanner: GoogleSQL
- AlloyDB: PostgreSQL
- Cloud SQL: MySQL or PostgreSQL (based on `db_type`)

## Key differences from BigQuery/Looker CA

- No data agent creation (`ca create-agent` not supported)
- No visualization rendering
- `--profile` is the only access method (no `--agent` or `--tables`)
