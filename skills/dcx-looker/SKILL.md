---
name: dcx-looker
description: Direct Looker commands — profile-driven explore/dashboard inspection via per-instance API. For GCP admin commands (instances, backups), see dcx-looker-admin-api.
---

## When to use this skill

Use when the user wants to:
- List or inspect Looker explores and dashboards
- Understand Looker content structure via profiles

Do not use for natural language questions — use **dcx-ca** instead.
For GCP admin commands (instances, backups), use **dcx-looker-admin-api**.

## Prerequisites

Content commands require a Looker profile with `source_type: looker`.
See **dcx-bigquery** for authentication.

## Commands

### List explores

```bash
dcx looker explores list --profile looker-sales.yaml --format json
```

### Get explore details

```bash
dcx looker explores get --profile looker-sales.yaml --explore model/explore_name --format json
```

### List dashboards

```bash
dcx looker dashboards list --profile looker-sales.yaml --format json
```

### Get dashboard details

```bash
dcx looker dashboards get --profile looker-sales.yaml --dashboard-id 42 --format json
```

## Decision rules

- Use `dcx looker explores|dashboards` for BI content inspection via profile
- Use `dcx ca ask --profile` for natural language Looker data exploration
- Content commands use per-instance Looker API (`https://<instance>.cloud.looker.com/api/4.0/`)
- Content commands require `--profile`; admin commands require `--project-id`

## Constraints

- Read-only: no create, update, or delete operations
- Requires a valid Looker profile with `source_type: looker`
- Explore reference format must be `model/explore` (with slash separator)
