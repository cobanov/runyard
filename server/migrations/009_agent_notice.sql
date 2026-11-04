-- When runyard last told people an agent is waiting for them, so each wait is told once.
ALTER TABLE agent_sessions ADD COLUMN told_at timestamptz;
