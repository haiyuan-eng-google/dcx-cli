---
name: dcx-databases
description: Direct inventory, schema, and database commands for AlloyDB, Spanner, and Cloud SQL via Discovery-driven APIs. Use for deterministic listing, metadata, and schema inspection — not natural language queries.
---

## When to use this skill

Use when the user wants to:
- List instances, clusters, or databases in AlloyDB, Spanner, or Cloud SQL
- Get metadata for a specific resource
- Describe schema columns via a profile
- Retrieve DDL (Spanner only)
- Perform deterministic inventory checks

Do not use for natural language questions — use **dcx-ca** instead.

## Prerequisites

All commands require `--project-id` (or `DCX_PROJECT`).
See **dcx-bigquery** for authentication and global flags.

## Command routing

| User goal | Command |
|-----------|---------|
| List Spanner instances | `dcx spanner instances list` |
| List Spanner databases | `dcx spanner databases list --instance-id ID` |
| Get Spanner DDL | `dcx spanner databases get-ddl --instance-id ID --database-id DB` |
| Describe Spanner schema | `dcx spanner schema describe --profile PROFILE` |
| List AlloyDB clusters | `dcx alloydb clusters list` |
| List AlloyDB instances | `dcx alloydb instances list --location LOC --cluster-id CL` |
| Describe AlloyDB schema | `dcx alloydb schema describe --profile PROFILE` |
| List AlloyDB databases | `dcx alloydb databases list --profile PROFILE` |
| List Cloud SQL instances | `dcx cloudsql instances list` |
| List Cloud SQL databases | `dcx cloudsql databases list --instance=INST` |
| Describe Cloud SQL schema | `dcx cloudsql schema describe --profile PROFILE` |

## Source type differences

| Service | Namespace | Location default | Schema command | Dialect |
|---------|-----------|-----------------|----------------|---------|
| Spanner | `spanner` | N/A | `schema describe --profile` | GoogleSQL |
| AlloyDB | `alloydb` | `-` (all) | `schema describe --profile` | PostgreSQL |
| Cloud SQL | `cloudsql` | N/A | `schema describe --profile` | MySQL or PostgreSQL |

## Profile-aware commands

Schema describe and database listing use CA QueryData under the hood, routed by the profile's `source_type`:

```bash
dcx spanner schema describe --profile spanner-finance.yaml --format table
dcx alloydb schema describe --profile alloydb-ops.yaml --format json
dcx alloydb databases list --profile alloydb-ops.yaml --format text
dcx cloudsql schema describe --profile cloudsql-app.yaml --format table
```

## Dry-run mode

Verify URL construction without auth:

```bash
dcx spanner instances list --project-id my-project --dry-run
```

## Decision rules

- Use `dcx <service> ...` for deterministic inventory and metadata
- Use `dcx <service> schema describe --profile` for column-level metadata
- Use `dcx ca ask --profile` for natural language data exploration
- AlloyDB `--location` defaults to `-` (all locations) when omitted
- Cloud SQL uses `--instance` (not `--instance-id`) for Discovery commands

## Constraints

- Read-only: no create, update, or delete operations
- Schema/database commands require a valid profile with matching `source_type`
- AlloyDB profile requires `source_type: alloy_db` (underscore, not hyphen)
- Cloud SQL profile requires `db_type` (`mysql` or `postgresql`)
- AlloyDB database listing filters out template databases automatically

## References

- See **dcx-spanner-api**, **dcx-alloydb-api**, **dcx-cloudsql-api** for per-service API command details
