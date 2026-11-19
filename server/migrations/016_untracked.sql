-- Training that nobody logs: a process that has kept a GPU busy for ten minutes on a machine with no running run.
-- The beacon's readings are watched in the server's memory; what was flagged is kept here, so the Machines page
-- shows it and a restart does not notify it again. Process names in settings 'untracked_ignore' (a JSON list,
-- added from the Machines page) are services, not trainings.
ALTER TABLE hosts ADD COLUMN untracked jsonb NOT NULL DEFAULT '[]';
