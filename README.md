# runyard

A lightweight, self-hosted training tracker. One Go binary plus PostgreSQL, started with `docker compose up`.
It shows each training run's status, metrics, samples and logs, sends phone notifications, and lets agents
read everything over MCP and a `--json` CLI. Far fewer features than W&B or MLflow, on purpose.

> Status: early development (v0.1). Not ready for use yet.

## Layout

| Path | What |
|---|---|
| `server/` | Go server: REST API, SSE, MCP, notifications, embedded web UI |
| `web/` | Svelte + Vite web UI (built into `server/webdist/dist`) |
| `sdk/python/` | Python client library and CLI (`pip install runyard`) |

## Development

```sh
(cd web && npm install && npm run build)
(cd server && go test ./... && go run .)
(cd sdk/python && uv venv && uv pip install -e '.[dev]' && .venv/bin/pytest)
```

## License

Apache-2.0
