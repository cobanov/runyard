CREATE TABLE projects (
  id         bigserial PRIMARY KEY,
  name       text NOT NULL UNIQUE,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE runs (
  id                uuid PRIMARY KEY, -- chosen by the client so a run can start while the server is down
  project_id        bigint NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  name              text NOT NULL,
  status            text NOT NULL DEFAULT 'running' CHECK (status IN ('running', 'finished', 'failed', 'crashed')),
  config            jsonb NOT NULL DEFAULT '{}',
  summary           jsonb NOT NULL DEFAULT '{}',
  tags              text[] NOT NULL DEFAULT '{}',
  notes             text NOT NULL DEFAULT '',
  host              text NOT NULL DEFAULT '',
  git               jsonb,
  started_at        timestamptz NOT NULL DEFAULT now(),
  ended_at          timestamptz,
  last_heartbeat_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX runs_project_started ON runs (project_id, started_at DESC);
CREATE INDEX runs_running ON runs (last_heartbeat_at) WHERE status = 'running';

CREATE TABLE metric_keys (
  id         bigserial PRIMARY KEY,
  project_id bigint NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  name       text NOT NULL,
  UNIQUE (project_id, name)
);

CREATE TABLE metrics (
  run_id uuid NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
  key_id bigint NOT NULL REFERENCES metric_keys(id) ON DELETE CASCADE,
  step   bigint NOT NULL,
  ts     timestamptz NOT NULL,
  value  double precision NOT NULL,
  PRIMARY KEY (run_id, key_id, step)
);

CREATE TABLE logs (
  run_id uuid NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
  seq    bigint NOT NULL, -- chosen by the client, makes retries idempotent
  ts     timestamptz NOT NULL,
  stream smallint NOT NULL DEFAULT 1, -- 1 stdout, 2 stderr
  line   text NOT NULL,
  PRIMARY KEY (run_id, seq)
);

CREATE TABLE media (
  id         uuid PRIMARY KEY,
  run_id     uuid NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
  key        text NOT NULL,
  step       bigint NOT NULL,
  kind       text NOT NULL CHECK (kind IN ('image', 'video', 'text')),
  path       text NOT NULL, -- relative to MEDIA_DIR
  meta       jsonb NOT NULL DEFAULT '{}',
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX media_run_key_step ON media (run_id, key, step);

CREATE TABLE alerts (
  id     bigserial PRIMARY KEY,
  run_id uuid NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
  ts     timestamptz NOT NULL DEFAULT now(),
  level  text NOT NULL CHECK (level IN ('info', 'warn', 'error')),
  title  text NOT NULL,
  body   text NOT NULL DEFAULT ''
);
CREATE INDEX alerts_run_ts ON alerts (run_id, ts);

CREATE TABLE channels (
  id     bigserial PRIMARY KEY,
  kind   text NOT NULL CHECK (kind IN ('ntfy', 'webhook')),
  config jsonb NOT NULL DEFAULT '{}'
);

CREATE TABLE rules (
  id         bigserial PRIMARY KEY,
  project_id bigint REFERENCES projects(id) ON DELETE CASCADE, -- NULL: every project
  event      text NOT NULL CHECK (event IN ('finished', 'failed', 'crashed', 'alert')),
  condition  jsonb NOT NULL DEFAULT '{}',
  channel_id bigint NOT NULL REFERENCES channels(id) ON DELETE CASCADE
);

CREATE TABLE api_keys (
  id         bigserial PRIMARY KEY,
  name       text NOT NULL,
  hash       text NOT NULL UNIQUE, -- sha256 hex of the key
  scopes     text[] NOT NULL DEFAULT '{write}',
  created_at timestamptz NOT NULL DEFAULT now()
);
