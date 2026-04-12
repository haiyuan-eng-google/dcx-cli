-- BigQuery benchmark seed data.
--
-- Run via: bq query --use_legacy_sql=false < seed.sql
-- Or use scripts/seed_bigquery.sh which handles dataset creation too.

-- Narrow events table: simple schema for basic read benchmarks.
CREATE TABLE IF NOT EXISTS dcx_benchmark.narrow_events (
  event_id STRING NOT NULL,
  event_type STRING NOT NULL,
  created_at TIMESTAMP NOT NULL,
  value FLOAT64
);

INSERT INTO dcx_benchmark.narrow_events (event_id, event_type, created_at, value)
VALUES
  ('evt-001', 'click', TIMESTAMP '2024-01-01 00:00:00 UTC', 1.0),
  ('evt-002', 'view', TIMESTAMP '2024-01-01 00:01:00 UTC', 2.5),
  ('evt-003', 'click', TIMESTAMP '2024-01-01 00:02:00 UTC', 1.0),
  ('evt-004', 'purchase', TIMESTAMP '2024-01-01 00:03:00 UTC', 49.99),
  ('evt-005', 'view', TIMESTAMP '2024-01-01 00:04:00 UTC', 2.5),
  ('evt-006', 'click', TIMESTAMP '2024-01-01 00:05:00 UTC', 1.0),
  ('evt-007', 'view', TIMESTAMP '2024-01-01 00:06:00 UTC', 2.5),
  ('evt-008', 'purchase', TIMESTAMP '2024-01-01 00:07:00 UTC', 99.99),
  ('evt-009', 'click', TIMESTAMP '2024-01-01 00:08:00 UTC', 1.0),
  ('evt-010', 'view', TIMESTAMP '2024-01-01 00:09:00 UTC', 2.5);

-- Wide metrics table: many columns for schema-depth benchmarks.
CREATE TABLE IF NOT EXISTS dcx_benchmark.wide_metrics (
  metric_id STRING NOT NULL,
  region STRING,
  service STRING,
  cpu_usage FLOAT64,
  memory_usage FLOAT64,
  disk_read_bytes INT64,
  disk_write_bytes INT64,
  network_in_bytes INT64,
  network_out_bytes INT64,
  request_count INT64,
  error_count INT64,
  latency_p50_ms FLOAT64,
  latency_p95_ms FLOAT64,
  latency_p99_ms FLOAT64,
  recorded_at TIMESTAMP NOT NULL
);

INSERT INTO dcx_benchmark.wide_metrics
  (metric_id, region, service, cpu_usage, memory_usage,
   disk_read_bytes, disk_write_bytes, network_in_bytes, network_out_bytes,
   request_count, error_count, latency_p50_ms, latency_p95_ms, latency_p99_ms,
   recorded_at)
VALUES
  ('m-001', 'us-central1', 'api', 0.45, 0.72, 1024, 512, 2048, 1024, 1000, 5, 12.3, 45.6, 120.0, TIMESTAMP '2024-01-01 00:00:00 UTC'),
  ('m-002', 'us-east1', 'worker', 0.80, 0.91, 4096, 2048, 8192, 4096, 500, 12, 25.0, 80.0, 200.0, TIMESTAMP '2024-01-01 00:00:00 UTC'),
  ('m-003', 'eu-west1', 'api', 0.30, 0.55, 512, 256, 1024, 512, 2000, 2, 8.0, 30.0, 90.0, TIMESTAMP '2024-01-01 00:00:00 UTC');
