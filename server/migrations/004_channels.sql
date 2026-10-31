-- More notification channels: Slack, Discord, Telegram, and web push to the installed web app (PWA).
ALTER TABLE channels DROP CONSTRAINT channels_kind_check;
ALTER TABLE channels ADD CONSTRAINT channels_kind_check CHECK (kind IN ('ntfy', 'webhook', 'slack', 'discord', 'telegram', 'webpush'));

-- Server-wide values made once, like the web push (VAPID) key pair.
CREATE TABLE settings (key text PRIMARY KEY, value text NOT NULL);
