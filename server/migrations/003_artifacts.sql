-- Artifacts: checkpoints, models and other files a run produced. Either a record of where the file lives (uri, a path on
-- the training machine or a bucket URL) or an uploaded copy (path, a key in the blob store), or both.
CREATE TABLE artifacts (
  id         uuid PRIMARY KEY, -- chosen by the client, makes retries idempotent
  run_id     uuid NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
  name       text NOT NULL,    -- "checkpoint-5000", or a relative path for MLflow ("model/MLmodel")
  kind       text NOT NULL DEFAULT 'file', -- checkpoint, model, mlflow, ... free text
  step       bigint,
  uri        text NOT NULL DEFAULT '',
  path       text,             -- uploaded copy: <run id>/artifacts/<id><ext>
  size       bigint,
  meta       jsonb NOT NULL DEFAULT '{}',
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX artifacts_run ON artifacts (run_id, created_at);
CREATE UNIQUE INDEX artifacts_run_name_mlflow ON artifacts (run_id, name) WHERE kind = 'mlflow'; -- MLflow overwrites by path
