-- Checks turn incoming data into alerts: a metric that goes NaN or crosses a threshold, a run that stops making progress.
CREATE TABLE checks (
  id         bigserial PRIMARY KEY,
  project_id bigint REFERENCES projects(id) ON DELETE CASCADE, -- NULL: every project
  kind       text NOT NULL CHECK (kind IN ('nan', 'above', 'below', 'stall')),
  key        text NOT NULL DEFAULT '', -- metric name, * matches anything; empty for stall
  value      double precision,         -- the threshold, or minutes for stall
  after_step bigint NOT NULL DEFAULT 0, -- steps before this are not checked (warmup)
  level      text NOT NULL DEFAULT 'warn' CHECK (level IN ('info', 'warn', 'error'))
);

ALTER TABLE alerts ADD COLUMN check_id bigint REFERENCES checks(id) ON DELETE SET NULL;
CREATE UNIQUE INDEX alerts_run_check ON alerts (run_id, check_id); -- a check alerts once per run; NULLs (run.alert) never collide

-- The last metric outside sys/ or output line. Heartbeats and the system sampler run on their own thread and keep
-- coming while a training loop hangs, so they do not count.
ALTER TABLE runs ADD COLUMN progress_at timestamptz NOT NULL DEFAULT now();

-- Only losses by default: fp16 training logs grad_norm as inf whenever the loss scaler skips a step.
INSERT INTO checks (kind, key, level) VALUES ('nan', '*loss*', 'error');
