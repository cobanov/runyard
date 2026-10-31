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
with `docker compose exec app /runyard-server`.

## Backup

`docker compose exec db pg_dump -U runyard runyard > runyard.sql`, plus the `media` volume (or the bucket).

## Server commands

```text
runyard-server                      serve
runyard-server key create [-user U] NAME
runyard-server channel add|ls|rm|test ...   see Notifications
runyard-server check add|ls|rm ...          see Notifications
runyard-server user|team|project ...        see above
```
