-- MLflow clients send no heartbeat: their writes count as signs of life, and their runs crash only after this much
-- silence (a long evaluation must not look like a dead process). NULL: the normal heartbeat window.
ALTER TABLE runs ADD COLUMN crash_after interval;
