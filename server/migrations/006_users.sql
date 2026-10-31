-- Users and teams, used when RUNYARD_AUTH is on. A project with a team is seen only by that team's members
-- (and admins); a project without one, by every user. A key with a user acts as that user.
CREATE TABLE users (
  id         bigserial PRIMARY KEY,
  name       text NOT NULL UNIQUE,
  password   text NOT NULL, -- pbkdf2-sha256$<iterations>$<salt>$<hash>
  admin      boolean NOT NULL DEFAULT false,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE teams (
  id         bigserial PRIMARY KEY,
  name       text NOT NULL UNIQUE,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE members (
  team_id bigint NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
  user_id bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  PRIMARY KEY (team_id, user_id)
);

CREATE TABLE sessions (
  hash       text PRIMARY KEY, -- sha256 hex of the cookie
  user_id    bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  expires_at timestamptz NOT NULL
);

ALTER TABLE api_keys ADD COLUMN user_id bigint REFERENCES users(id) ON DELETE CASCADE;
-- no ON DELETE: removing a team must not open its projects to everyone
ALTER TABLE projects ADD COLUMN team_id bigint REFERENCES teams(id);
-- a push subscription made while logged in gets only what its user may see
ALTER TABLE channels ADD COLUMN user_id bigint REFERENCES users(id) ON DELETE CASCADE;
