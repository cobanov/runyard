# make test: every test against a real Postgres in docker (container runyard-test-pg, port 55432).
PG := runyard-test-pg
export RUNYARD_TEST_DATABASE_URL := postgres://postgres:test@localhost:55432/postgres?sslmode=disable

.PHONY: web server test test-db

web:
	cd web && npm install --silent && npm run build

server: web
	cd server && go build -o runyard-server .

test-db:
	@docker start $(PG) >/dev/null 2>&1 || docker run -d --name $(PG) -p 55432:5432 -e POSTGRES_PASSWORD=test postgres:17 >/dev/null
	@until docker exec $(PG) pg_isready -U postgres >/dev/null 2>&1; do sleep 0.5; done

test: test-db
	cd server && go vet ./... && go test -count=1 ./...
	cd sdk/python && .venv/bin/pytest -q
