-- Why a run ended badly, in one line: the exception and the first line of its message ("torch.OutOfMemoryError: CUDA
-- out of memory. ..."), or what killed a wrapped command; and the process's exit code. Sent with the run's end by
-- clients newer than 0.7; older ones leave both empty, and a run that starts again clears them.
ALTER TABLE runs ADD COLUMN error text NOT NULL DEFAULT '';
ALTER TABLE runs ADD COLUMN exit_code int;
