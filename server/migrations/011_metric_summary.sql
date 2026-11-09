-- The last, lowest and highest value of every metric of a run, kept up to date as metrics arrive. Run pages, lists
-- and the apps read this instead of scanning and sorting a run's whole history on every poll.
CREATE TABLE metric_summary (
  run_id uuid NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
  key_id bigint NOT NULL,
  count  bigint NOT NULL,
  first  bigint NOT NULL, -- the first step
  step   bigint NOT NULL, -- the last step
  last   double precision NOT NULL,
  min    double precision NOT NULL,
  max    double precision NOT NULL,
  PRIMARY KEY (run_id, key_id)
);
-- Existing runs: one pass over the metrics table, which takes a while on a large database, once.
INSERT INTO metric_summary
  SELECT run_id, key_id, count(*), min(step), max(step), (array_agg(value ORDER BY step DESC))[1], min(value), max(value) FROM metrics GROUP BY 1, 2;

-- Deleting a project deleted its metric keys, and each key then searched the whole metrics table for its rows,
-- with no index to do it by. The rows go with their runs anyway.
ALTER TABLE metrics DROP CONSTRAINT IF EXISTS metrics_key_id_fkey;
CREATE INDEX IF NOT EXISTS agent_sessions_report_run ON agent_sessions (report_run);
