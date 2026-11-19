-- How many steps a run will take, when its script says (ry.init(total_steps=...), run.progress(total=...)): the
-- progress bar and the time left use it before guessing from config names like max_steps. NULL: not said.
ALTER TABLE runs ADD COLUMN total_steps bigint;
