# ca create-agent Command Reference

## Usage

```bash
dcx ca create-agent \
  --name=<AGENT_NAME> \
  --tables=<TABLE_REFS> \
  [--views=<VIEW_REFS>] \
  [--verified-queries=<PATH>] \
  [--instructions=<TEXT>]
```

## Flags

| Flag | Required | Description |
|------|----------|-------------|
| `--name` | Yes | Agent ID: lowercase letters, digits, hyphens; must start with a letter (`^[a-z]([a-z0-9-]{0,61}[a-z0-9])?$`) |
| `--tables` | Yes | Comma-separated fully qualified table refs |
| `--views` | No | Comma-separated view refs as additional data sources |
| `--verified-queries` | No | Path to verified queries YAML file |
| `--instructions` | No | System instructions for the agent |

## Verified queries format

```yaml
verified_queries:
  - question: "What is the error rate for {agent}?"
    query: |
      SELECT SAFE_DIVIDE(
        COUNTIF(ENDS_WITH(event_type, '_ERROR')),
        COUNT(DISTINCT session_id)
      ) AS error_rate
      FROM `{project}.{dataset}.agent_events`
      WHERE agent = @agent
        AND timestamp >= TIMESTAMP_SUB(CURRENT_TIMESTAMP(), INTERVAL 24 HOUR)
```

## Example

```bash
dcx ca create-agent \
  --name=agent-analytics \
  --tables=myproject.analytics.agent_events \
  --views=myproject.analytics.adk_llm_response \
  --verified-queries=./deploy/ca/verified_queries.yaml \
  --instructions="You help analyze AI agent performance."
```

## Adding queries incrementally

```bash
dcx ca add-verified-query \
  --agent=agent-analytics \
  --question="What is the error rate for {agent}?" \
  --query="SELECT ..."
```

## Notes

- Agent management always uses `locations/global` — the `--location` flag is ignored
- Requires `roles/geminidataanalytics.dataAgentCreator` (or `roles/owner`)
- Requires `geminidataanalytics.googleapis.com` and `cloudaicompanion.googleapis.com` APIs enabled
- Table refs must be fully qualified: `project.dataset.table`
- Only supports BigQuery tables/views
