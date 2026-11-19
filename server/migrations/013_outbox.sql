-- Notifications on their way. A row is written before the first try and deleted once the channel took it; a failed
-- try is tried again later (1 m, 5 m, 30 m, 2 h, 6 h), also after a server restart. A row with next_at NULL was given
-- up on: the Alerts page lists it as not delivered until someone dismisses it, and it is deleted after 30 days.
CREATE TABLE outbox (
  id           bigserial PRIMARY KEY,
  channel_id   bigint NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
  run_id       uuid REFERENCES runs(id) ON DELETE CASCADE, -- NULL for a machine notice
  message      jsonb NOT NULL, -- what is sent, see queued in notify.go
  created_at   timestamptz NOT NULL DEFAULT now(),
  attempts     int NOT NULL DEFAULT 0,
  next_at      timestamptz,
  last_error   text NOT NULL DEFAULT '',
  dismissed_at timestamptz
);
CREATE INDEX outbox_due ON outbox (next_at) WHERE next_at IS NOT NULL;
