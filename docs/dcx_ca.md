## Summary

`dcx` is no longer a Rust prototype pitch. It is now a shipped **Go CLI** for agent-native Data Cloud work: **86 commands across 11 domains**, generated skills, structured output, typed errors, MCP bridge, and dogfooding against real GCP APIs.

The strongest current product framing is:

> **dcx lets a general agent complete the full Conversational Analytics (CA) journey across the whole Data Cloud through one CLI and generated skills.**

That means an agent can discover sources, inspect schemas, create or reuse CA agents, ask natural-language questions, monitor resources, and clean up — without having to know whether the backend is BigQuery, Looker, Spanner, AlloyDB, or Cloud SQL.

## Current State

| Area | Current state |
|---|---|
| Implementation | Go rewrite, not Rust prototype |
| Surface | 86 commands across 11 domains |
| Data Cloud coverage | BigQuery, Spanner, AlloyDB, Cloud SQL, Looker |
| CA coverage | `ca ask` across all source profiles; BigQuery agent lifecycle commands |
| Agent interface | Generated `SKILL.md` files, machine-readable contracts, structured JSON |
| MCP | `dcx mcp serve`, 57 read-only tools over JSON-RPC 2.0 |
| Dogfooding | 65/86 commands executed against real APIs; 21 skipped only for missing AlloyDB/Looker infra |
| Bugs found/fixed | 5 UX/behavior issues from dogfooding (#37-42) |

This changes the issue from “why should dcx exist?” to “dcx exists; here is the agent-native Data Cloud control plane it exposes.”

## Core Thesis

`bq` is a strong human CLI for BigQuery. `dcx` is an agent-native CLI for the broader Data Cloud.

The difference is not just command coverage. The difference is the **interface contract**:

| Need | Traditional CLI | dcx |
|---|---|---|
| Agent discovery | Human help text | Generated skills + `meta describe` |
| Output | Human tables by default | Structured JSON / `json-minified` |
| Errors | Human-readable stderr | Typed JSON error envelope + semantic exit codes |
| Cross-source work | Separate tools per product | One binary across Data Cloud |
| CA journey | Agent must know backend APIs | `dcx ca ask` routes by profile/source type |
| Tool overhead | MCP registers many tools | CLI uses the shell tool agents already have |

MCP is still useful when the host environment requires API-style tool calls. But when an agent has a shell, a CLI is the lower-overhead control plane: one shell tool, many Data Cloud workflows.

## CA Across The Whole Data Cloud

The key CA differentiator is that **one `dcx ca ask` surface works across all Data Cloud source families**. The profile determines routing. The agent does not need to choose between Chat/DataAgent API and QueryData API.

```bash
# BigQuery — Chat/DataAgent path
dcx ca ask "top errors yesterday" --agent=support-agent --project-id=P

# Looker — Chat/DataAgent path
dcx ca ask "top selling products" --profile=sales-looker.yaml

# Spanner — QueryData path
dcx ca ask "revenue by region" --profile=finance-spanner.yaml

# AlloyDB — QueryData path
dcx ca ask "show all tables" --profile=ops-alloydb.yaml

# Cloud SQL — QueryData path
dcx ca ask "active users today" --profile=app-cloudsql.yaml
```

Supported profile source types:

| Source | Profile type | CA/API family |
|---|---|---|
| BigQuery | `bigquery` | Chat / DataAgent |
| Looker | `looker` | Chat / DataAgent |
| Looker Studio | `looker_studio` | Chat / DataAgent |
| Spanner | `spanner` | QueryData |
| AlloyDB | `alloy_db` | QueryData |
| Cloud SQL | `cloud_sql` | QueryData |

## Full CA Journey For A General Agent

With generated dcx skills, a general agent can follow the entire CA workflow:

| Journey step | Command examples | Agent value |
|---|---|---|
| **Discover sources** | `dcx profiles list`, `dcx profiles validate`, `dcx profiles test` | Find usable Data Cloud profiles and verify auth/connectivity |
| **Inspect schema** | `dcx tables get --output-fields=schema`, `dcx spanner schema describe --profile=...`, `dcx cloudsql schema describe --profile=...` | Understand source shape before asking or creating agents |
| **Create CA agent** | `dcx ca create-agent --tables=... --instructions=...` | Build a BigQuery CA data agent |
| **Ground behavior** | `dcx ca add-verified-query --agent=X --question=... --query=...` | Add verified queries so answers stay grounded |
| **Ask questions** | `dcx ca ask "question" --agent=X`, `dcx ca ask "question" --profile=spanner.yaml` | Natural-language query over BigQuery or database/Looker sources |
| **Monitor** | `dcx ca list-agents --output-fields=_resource_id,display_name` | Enumerate reusable CA agents |
| **Cleanup** | `dcx ca delete-agent --name=X --force` | Remove test/demo agents safely |

The same workflow can be executed by humans, scripts, CI, or agents because the contract is stable: flags are discoverable, output is structured, and errors are typed.

## Skill-Guided Agent Use

dcx does not require every agent platform to hardcode Data Cloud syntax. It generates skills from the same command contract that powers the CLI, MCP tool schemas, and `meta describe`.

```bash
dcx meta generate-skills --domains bigquery,spanner,cloudsql,looker,ca
```

Generated skills include:

- command routing tables
- workflow recipes
- flag references
- output shape expectations
- decision rules for which command to use

Example generated workflow:

```text
Spanner schema migration:
1. dcx spanner databases get-ddl --database=...
2. dcx spanner databases update-ddl --ddl-file schema.sql --operation-id migration-001 --dry-run
3. dcx spanner databases update-ddl --ddl-file schema.sql --operation-id migration-001
4. dcx spanner operations wait --operation-name=$OP --timeout=120
5. dcx spanner databases get-ddl --database=...
```

This is the important agent point: the skill tells the agent **what to do next**, not just which API exists.

## Current Command Surface

| Domain | Commands | Read | Write | Key capabilities |
|---|---:|---:|---:|---|
| BigQuery | 15 | 11 | 4 | datasets/tables CRUD, jobs query, models, routines |
| Spanner | 18 | 13 | 5 | databases CRUD+DDL, backups CRUD, operations wait |
| AlloyDB | 14 | 12 | 2 | clusters, instances, backups, users CRUD |
| Cloud SQL | 17 | 13 | 4 | databases/users CRUD, backup-runs, flags, tiers |
| Looker | 6 | 6 | 0 | instances, backups, explores, dashboards |
| CA | 5 | 2 | 3 | ask across all sources, agent lifecycle |
| Meta | 4 | 4 | 0 | commands, describe, generate-skills, schema |
| Other | 7 | 7 | 0 | auth, profiles, MCP serve, completion |

Notable shipped features:

- `meta commands` and `meta describe` for machine-readable command discovery
- `meta schema` for request body construction and `$ref` expansion
- `--format json`, `json-minified`, `table`, `text`
- `--output-fields` to shrink responses for agents
- `_resource_id` on list output for command chaining
- `--dry-run`, `--force`, `--no-validate` for safe unattended mutation flows
- structured JSON errors with retryability and semantic exit codes
- `--retry` with silent exponential backoff
- long-running operation polling via commands like `spanner operations wait`
- `dcx mcp serve` exposing read-only tools while excluding mutations/wait commands

## Architecture

dcx is implemented in Go with one contract model powering:

- CLI command registration
- `meta commands`
- `meta describe`
- generated skills
- MCP tool schemas
- validation and output contracts

Commands are generated from bundled Google Cloud Discovery Documents where possible. There is no runtime Discovery fetch.

Adding a Discovery-backed API method is an allowlist change in service configuration:

```go
AllowedMethods: []string{
    "datasets.list",
    "datasets.get",
    "datasets.insert",
    "datasets.delete",
}
```

Static/helper commands such as `jobs query`, `ca ask`, schema helpers, Looker SDK helpers, and operation waiters register into the same contract model so agents see one consistent interface.

## Dogfooding Results

Systematic dogfooding in #41 used generated skills as the guide, the same way a general agent would.

| Metric | Result |
|---|---:|
| Commands executed against real APIs | 65 / 86 |
| Commands skipped only due to missing AlloyDB/Looker infra | 21 |
| Commands blocked | 0 |
| UX/behavior issues found and fixed | 5 (#37-42) |
| Output formats verified | 4 (`json`, `json-minified`, `table`, `text`) |
| MCP tools exposed | 57 read-only tools |

The skipped commands are structurally covered by the same command generation/runtime paths as executed commands; they require expensive AlloyDB/Looker infrastructure to exercise directly.

## Why This Matters

For CA and Data Cloud, a general agent needs more than a single API call. It needs a safe, discoverable journey:

1. Identify available sources.
2. Validate credentials/connectivity.
3. Inspect schema.
4. Create or select a CA agent.
5. Add verified queries when needed.
6. Ask natural-language questions.
7. Monitor and clean up.

dcx now exposes that journey through one shell-accessible contract. A general agent can use generated skills to operate across BigQuery, Looker, Spanner, AlloyDB, and Cloud SQL without bespoke wrappers for each backend.

That is the product claim to validate next: **dcx as the agent-native CA/Data Cloud control plane.**

## Follow-Ups

- Keep README command counts aligned with dogfooding coverage (`84` vs `86` discrepancy).
- Add more live AlloyDB/Looker coverage when infrastructure is available.
- Continue expanding CA skill recipes around common analyst/admin workflows.
- Consider a short demo: general agent uses generated dcx skills to discover a profile, inspect schema, ask a CA question, and clean up.
