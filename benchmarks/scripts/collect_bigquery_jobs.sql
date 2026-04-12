-- Collect BigQuery job telemetry for benchmark runs.
--
-- Run after a benchmark cycle to capture server-side metrics.
-- Replace {project} and {start_time} / {end_time} before executing.
--
-- Usage:
--   bq query --use_legacy_sql=false --format=json < collect_bigquery_jobs.sql

SELECT
  job_id,
  user_email,
  query,
  state,
  creation_time,
  start_time,
  end_time,
  total_bytes_processed,
  total_bytes_billed,
  total_slot_ms,
  cache_hit,
  TIMESTAMP_DIFF(end_time, start_time, MILLISECOND) AS duration_ms
FROM
  `{project}.region-us.INFORMATION_SCHEMA.JOBS`
WHERE
  creation_time BETWEEN TIMESTAMP('{start_time}') AND TIMESTAMP('{end_time}')
  AND job_type = 'QUERY'
  AND state = 'DONE'
ORDER BY
  creation_time ASC;
