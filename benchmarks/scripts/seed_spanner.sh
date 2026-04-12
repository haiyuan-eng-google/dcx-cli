#!/usr/bin/env bash
# Seed Spanner benchmark instance, database, and tables.
#
# Usage: ./seed_spanner.sh <project-id> [tier]
#
# Tiers:
#   small   — Tier A: 5 singers, 8 albums (default)
#   medium  — Tier B: 10,000 singers, 50,000 albums
#   large   — Tier C: 100,000 singers, 500,000 albums

set -euo pipefail

PROJECT="${1:?Usage: $0 <project-id> [small|medium|large]}"
TIER="${2:-small}"
INSTANCE="dcx-bench"
DATABASE="bench-music"
REGION="regional-us-central1"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

echo "==> Creating Spanner instance ${INSTANCE}..."
gcloud spanner instances create "${INSTANCE}" \
  --project="${PROJECT}" \
  --config="${REGION}" \
  --description="dcx benchmark" \
  --processing-units=100 \
  2>/dev/null || echo "    Instance already exists."

echo "==> Creating database ${DATABASE}..."
gcloud spanner databases create "${DATABASE}" \
  --project="${PROJECT}" \
  --instance="${INSTANCE}" \
  2>/dev/null || echo "    Database already exists."

echo "==> Applying schema..."
gcloud spanner databases ddl update "${DATABASE}" \
  --project="${PROJECT}" \
  --instance="${INSTANCE}" \
  --ddl-file="${SCRIPT_DIR}/../data/spanner/schema.sql" \
  2>/dev/null || echo "    Schema already applied."

if [ "${TIER}" = "small" ]; then
  echo "==> Seeding Tier A (small) data..."
  gcloud spanner databases execute-sql "${DATABASE}" \
    --project="${PROJECT}" \
    --instance="${INSTANCE}" \
    --sql="$(cat "${SCRIPT_DIR}/../data/spanner/seed.sql")"
elif [ "${TIER}" = "medium" ] || [ "${TIER}" = "large" ]; then
  if [ "${TIER}" = "medium" ]; then
    SINGER_COUNT=10000
    ALBUMS_PER_SINGER=5
  else
    SINGER_COUNT=100000
    ALBUMS_PER_SINGER=5
  fi
  echo "==> Seeding Tier ${TIER}: ${SINGER_COUNT} singers, $((SINGER_COUNT * ALBUMS_PER_SINGER)) albums..."
  echo "    (Generating deterministic data with sequential IDs)"

  # Generate and insert in batches of 1000.
  BATCH=1000
  for ((i = 1; i <= SINGER_COUNT; i += BATCH)); do
    END=$((i + BATCH - 1))
    if [ "${END}" -gt "${SINGER_COUNT}" ]; then END="${SINGER_COUNT}"; fi

    SINGER_SQL="INSERT INTO Singers (SingerId, FirstName, LastName, BirthDate) VALUES "
    ALBUM_SQL="INSERT INTO Albums (SingerId, AlbumId, AlbumTitle, ReleaseYear) VALUES "
    SEP=""
    ASEP=""

    for ((j = i; j <= END; j++)); do
      SINGER_SQL="${SINGER_SQL}${SEP}(${j}, 'First${j}', 'Last${j}', '1970-01-01')"
      SEP=","
      for ((k = 1; k <= ALBUMS_PER_SINGER; k++)); do
        ALBUM_SQL="${ALBUM_SQL}${ASEP}(${j}, ${k}, 'Album${j}_${k}', $((2000 + (j % 25))))"
        ASEP=","
      done
    done

    gcloud spanner databases execute-sql "${DATABASE}" \
      --project="${PROJECT}" \
      --instance="${INSTANCE}" \
      --sql="${SINGER_SQL}" 2>/dev/null

    gcloud spanner databases execute-sql "${DATABASE}" \
      --project="${PROJECT}" \
      --instance="${INSTANCE}" \
      --sql="${ALBUM_SQL}" 2>/dev/null

    printf "\r    Inserted %d / %d singers..." "${END}" "${SINGER_COUNT}"
  done
  echo ""
fi

echo "==> Verifying..."
gcloud spanner databases execute-sql "${DATABASE}" \
  --project="${PROJECT}" \
  --instance="${INSTANCE}" \
  --sql="SELECT COUNT(*) AS singer_count FROM Singers"

echo "==> Done. Spanner benchmark data is ready."
