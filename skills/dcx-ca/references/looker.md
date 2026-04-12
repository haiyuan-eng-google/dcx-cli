# Looker CA Setup

## Profile

```yaml
name: sales-looker
source_type: looker
project: my-gcp-project
looker_instance_url: https://mycompany.looker.com
looker_explores:
  - sales_model/orders
  - sales_model/customers
```

### With OAuth credentials

```yaml
name: sales-looker-oauth
source_type: looker
project: my-gcp-project
looker_instance_url: https://mycompany.looker.com
looker_explores:
  - sales_model/orders
looker_client_id: YOUR_CLIENT_ID
looker_client_secret: YOUR_CLIENT_SECRET
```

Both `looker_client_id` and `looker_client_secret` must be provided together or both omitted.

## Explore format

Explores use `model/explore` format:
- `sales_model/orders` — valid
- `just_an_explore` — invalid (missing model)
- `model/explore/extra` — invalid (too many segments)

## Constraints

- Maximum 5 explores per profile
- `looker_instance_url` required and must not be empty
- At least one explore required
- `ca create-agent` does not support Looker profiles — use `ca ask --profile` only
