-- Agent sessions: a coding agent (Claude Code, Codex, OpenCode, Gemini CLI, ...) seen through the runs it starts (the
-- SDK reads the session id its CLI puts in the environment) and through its hooks. id is "<tool>:<session>".
CREATE TABLE agent_sessions (
  id          text PRIMARY KEY,
  tool        text NOT NULL,
  label       text NOT NULL DEFAULT '', -- RUNYARD_AGENT, when a run named the agent by hand
  host        text NOT NULL DEFAULT '',
  cwd         text NOT NULL DEFAULT '',
  project_id  bigint REFERENCES projects(id) ON DELETE SET NULL, -- the project of its latest run
  hook        text NOT NULL DEFAULT '', -- what its hooks last said: working, waiting, idle, error or ended
  hook_at     timestamptz,
  card        boolean NOT NULL DEFAULT false, -- its hooks were installed for a repository: a card before its first run
  said        text NOT NULL DEFAULT '', -- its last message when a turn ended (the Stop hook)
  report      text NOT NULL DEFAULT '', -- the last report it wrote with the MCP report tool
  report_next text NOT NULL DEFAULT '',
  report_run  uuid REFERENCES runs(id) ON DELETE SET NULL,
  report_at   timestamptz,
  started_at  timestamptz NOT NULL DEFAULT now(),
  seen_at     timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE runs ADD COLUMN agent text REFERENCES agent_sessions(id) ON DELETE SET NULL;
CREATE INDEX runs_agent ON runs (agent, started_at) WHERE agent IS NOT NULL;

-- The number a project's runs are judged by and which way is better; '' leaves it to a loss.
ALTER TABLE projects ADD COLUMN metric text NOT NULL DEFAULT '',
  ADD COLUMN goal text NOT NULL DEFAULT 'min' CHECK (goal IN ('min', 'max'));
