-- What a run started as: command line, Python, OS, hardware and the uncommitted diff. Only a run's own page reads it.
ALTER TABLE runs ADD COLUMN env jsonb;
