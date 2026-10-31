# Run the server

```sh
docker compose up -d --build
docker compose exec app /runyard-server key create my-laptop
```

The image is one static binary with the web UI embedded; `docker compose` adds PostgreSQL 17. Migrations run at
start. Released images are at `ghcr.io/cobanov/runyard`.

## Reaching it

Without `RUNYARD_AUTH`, reading needs no key, so the port is bound to `127.0.0.1` by default. Reach it from other
machines through `tailscale serve --bg --https=443 http://127.0.0.1:8080` or a tunnel, or set
`RUNYARD_BIND=0.0.0.0:8080`. Push notifications and installing the web app need HTTPS (or localhost).

## Settings

Environment variables of the server (in `.env` next to `docker-compose.yml`):

| Variable | Meaning |
|---|---|
| `DATABASE_URL` | PostgreSQL connection string |
| `ADDR` | listen address, default `:8080` |
| `RUNYARD_BIND` | host port binding in compose, default `127.0.0.1:8080` |
| `RUNYARD_URL` | public address, for links in notifications |
| `RUNYARD_AUTH` | `1`: reads need a login or key; teams see their own projects |
| `MEDIA_DIR` | where images, videos and uploaded artifacts go, default `./media` (`/media` volume in compose) |
| `S3_BUCKET`, `S3_ENDPOINT`, `S3_REGION`, `S3_PREFIX`, `S3_ACCESS_KEY_ID`, `S3_SECRET_ACCESS_KEY` | keep those files in an S3-compatible bucket instead |
| `RUNYARD_PUSH_CONTACT` | `mailto:` or `https:` address push services may contact, default the project page |

## Media in S3 or Cloudflare R2

Set `S3_BUCKET` and its credentials; `S3_ENDPOINT` is `https://<account id>.r2.cloudflarestorage.com` for R2 and
`S3_REGION` stays `auto`. The bucket stays private: files are streamed through the server, so links do not change.
One object is at most 5 GB.

## Users and teams

With `RUNYARD_AUTH=1` every API, media, event and MCP request needs a user: the web UI shows a login form, scripts
and agents send an API key. Admins see every project; other users see projects without a team and their teams'
projects. A user's new project goes to their first team.

```sh
runyard-server user add -admin mert        # prints a generated password
runyard-server user add ege
runyard-server user passwd ege             # a new password, and logs ege out everywhere
runyard-server team add vision
runyard-server team join vision ege        # team leave vision ege
runyard-server project team sft vision     # only vision (and admins) see sft; - for everyone
runyard-server key create -user ege ege-laptop
runyard-server user ls; runyard-server team ls
```

A key made without `-user` acts as the server: it sees and writes everything. Inside compose, prefix the commands
with `docker compose exec app /runyard-server`. Logged in as an admin, the web UI's Settings page makes and revokes
keys too. Without `RUNYARD_AUTH`, the Settings page asks for an API key instead, kept for that browser tab only.

Editing a run's notes and tags and dismissing alerts, from the web UI or the app, need no admin: with
`RUNYARD_AUTH`, any user who can see the run; without it, anyone, as anyone can read.

## Machines

`runyard agent` on each machine you train on sends what it is doing every 10 seconds, training or not: per GPU the
utilization, memory, temperature, power and every process on it with its user, plus CPU, memory and disk. The web
UI's Machines page and the iPhone app's Server tab show them, the Overview shows each machine's load, and the server
tells the channels that hear every project's alerts when a machine has sent nothing for 2 minutes, and when it is
back. A day of readings is kept.

```sh
pip install "runyard[system]"        # nvidia-ml-py for the GPUs, psutil for the rest
RUNYARD_SERVER=https://my-server RUNYARD_API_KEY=ry_... runyard agent
```

The machine's name is its hostname, the one runs record, so the page shows which run is on which machine (`--name`
changes it). A process shows as its program and script, `python train.py`, never its whole command line.

To keep it running on Linux, as a systemd user service that starts at boot:

```sh
mkdir -p ~/.config/runyard ~/.config/systemd/user
printf 'RUNYARD_SERVER=https://my-server\nRUNYARD_API_KEY=ry_...\n' > ~/.config/runyard/agent.env
chmod 600 ~/.config/runyard/agent.env
cat > ~/.config/systemd/user/runyard-agent.service <<'UNIT'
[Unit]
Description=runyard agent
After=network-online.target

[Service]
EnvironmentFile=%h/.config/runyard/agent.env
ExecStart=%h/.local/bin/runyard agent
Restart=always
RestartSec=10

[Install]
WantedBy=default.target
UNIT
systemctl --user enable --now runyard-agent
loginctl enable-linger "$USER"       # start without a login
```

`ExecStart` is wherever `runyard` is installed (`which runyard`). On macOS, a launchd agent in
`~/Library/LaunchAgents/runyard.agent.plist` (then `chmod 600` it, as it holds the key, and
`launchctl load ~/Library/LaunchAgents/runyard.agent.plist`):

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>Label</key><string>runyard.agent</string>
  <key>ProgramArguments</key><array><string>/path/to/runyard</string><string>agent</string></array>
  <key>EnvironmentVariables</key><dict>
    <key>RUNYARD_SERVER</key><string>https://my-server</string>
    <key>RUNYARD_API_KEY</key><string>ry_...</string>
  </dict>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
</dict></plist>
```

A machine you retire stays on the page as silent; remove it there, or with `DELETE /api/v1/hosts/{name}`.

## Admin API

Deleting and the server's keys and channels, for scripts and agents. With `RUNYARD_AUTH`, the caller is an admin
(session cookie or key) or a key made without `-user`; without it, any API key. Others get 403.

| Endpoint | Does |
|---|---|
| `DELETE /api/v1/runs/{id}` | deletes a run (exact id) with its metrics, logs, alerts, media and artifact files |
| `DELETE /api/v1/projects/{name}` | the same for every run of the project, then the project |
| `DELETE /api/v1/hosts/{name}` | removes a machine and its readings from the Machines page |
| `GET /api/v1/keys`, `POST /api/v1/keys` `{"name", "user"}`, `DELETE /api/v1/keys/{id}` | list (never the key), create (returns the key once), revoke |
| `GET /api/v1/channels`, `POST /api/v1/channels`, `DELETE /api/v1/channels/{id}`, `POST /api/v1/channels/{id}/test` | as `channel ls/add/rm/test`; the list leaves out tokens |

`POST /api/v1/channels` takes `{"kind", "target", "token", "project", "events"}`: target is the URL, or the chat
id for telegram; no project means every project, no events all four.

## Backup

`docker compose exec db pg_dump -U runyard runyard > runyard.sql`, plus the `media` volume (or the bucket).

## Server commands

```text
runyard-server                      serve
runyard-server key create [-user U] NAME
runyard-server channel add|ls|rm|test ...   see Notifications (or the web UI's Settings page)
runyard-server check add|ls|rm ...          see Notifications
runyard-server user|team|project ...        see above
```
