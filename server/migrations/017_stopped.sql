-- A run someone stopped (Ctrl-C, SIGTERM, a closed terminal, MLflow's KILLED) is stopped, not failed. Channels told
-- about failures are told about stops too, since until now those arrived as failures; at normal priority.
ALTER TABLE runs DROP CONSTRAINT runs_status_check;
ALTER TABLE runs ADD CONSTRAINT runs_status_check CHECK (status IN ('running', 'finished', 'failed', 'crashed', 'stopped'));
ALTER TABLE rules DROP CONSTRAINT rules_event_check;
ALTER TABLE rules ADD CONSTRAINT rules_event_check CHECK (event IN ('finished', 'failed', 'crashed', 'stopped', 'alert'));
INSERT INTO rules (project_id, event, condition, channel_id)
  SELECT project_id, 'stopped', condition, channel_id FROM rules WHERE event = 'failed';
