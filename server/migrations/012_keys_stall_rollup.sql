-- Keys get a second scope. A key with only 'write' is for a training machine: it logs and reads, and cannot delete
-- runs or manage keys and channels. Every key made so far keeps doing everything.
UPDATE api_keys SET scopes = '{write,admin}';

-- A run that sends heartbeats but no metric and no output line for an hour gets one warning. Runs logged through
-- MLflow (crash_after set) are left to stall checks made for their own project: they are quiet by nature.
INSERT INTO checks (kind, value, level)
  SELECT 'stall', 60, 'warn' WHERE NOT EXISTS (SELECT 1 FROM checks WHERE kind = 'stall' AND project_id IS NULL);

-- A long run's history in buckets of 100 steps, kept as metrics arrive like metric_summary: a chart of ten million
-- steps reads a hundred thousand rows instead of all of them. t is the unix time of the bucket's last step.
CREATE TABLE metric_rollup (
  run_id uuid NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
  key_id bigint NOT NULL,
  bucket bigint NOT NULL, -- step / 100
  step   bigint NOT NULL, -- the last step in it
  t      double precision NOT NULL,
  last   double precision NOT NULL,
  min    double precision NOT NULL,
  max    double precision NOT NULL,
  PRIMARY KEY (run_id, key_id, bucket)
);
INSERT INTO metric_rollup
  SELECT run_id, key_id, step / 100, max(step), (max(ARRAY[step::float8, extract(epoch FROM ts)::float8]))[2],
    (max(ARRAY[step::float8, value]))[2], min(value), max(value)
  FROM metrics GROUP BY 1, 2, 3;
