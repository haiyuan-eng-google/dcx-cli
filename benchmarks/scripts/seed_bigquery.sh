#!/usr/bin/env bash
# Seed BigQuery benchmark dataset and tables.
#
# Usage: ./seed_bigquery.sh <project-id>
#
# Creates the dcx_benchmark dataset and populates tables from seed.sql.

set -euo pipefail

PROJECT="${1:?Usage: $0 <project-id>}"
DATASET="dcx_benchmark"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
SEED_SQL="${SCRIPT_DIR}/../data/bigquery/seed.sql"

echo "==> Creating dataset ${DATASET} in project ${PROJECT}..."
bq mk --project_id="${PROJECT}" --dataset "${DATASET}" 2>/dev/null || echo "    Dataset already exists."

echo "==> Applying seed data..."
bq query \
  --project_id="${PROJECT}" \
  --use_legacy_sql=false \
  --format=none \
  < "${SEED_SQL}"

echo "==> Verifying tables..."
bq ls --project_id="${PROJECT}" "${DATASET}"

echo "==> Done. BigQuery benchmark data is ready."
