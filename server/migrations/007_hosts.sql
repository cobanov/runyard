-- Machines: `runyard agent` posts each host's GPU, CPU, memory and disk every few seconds, training or not.
CREATE TABLE hosts (
  name    text PRIMARY KEY,
  seen_at timestamptz NOT NULL DEFAULT now(),
  stats   jsonb NOT NULL DEFAULT '{}', -- the latest reading, as the agent sent it
  silent  boolean NOT NULL DEFAULT false -- the channels were told it went silent
);

-- A day of readings per host, flattened to numbers (cpu, ram_gb, gpu0.util, ...) for the charts.
CREATE TABLE host_samples (
  host text NOT NULL REFERENCES hosts(name) ON DELETE CASCADE,
  ts   timestamptz NOT NULL DEFAULT now(),
  vals jsonb NOT NULL,
  PRIMARY KEY (host, ts)
);

-- An alert someone has seen and put away: it leaves the Overview, and the Alerts page shows it dimmed.
ALTER TABLE alerts ADD COLUMN dismissed_at timestamptz;
