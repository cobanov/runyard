"""Fixtures that run the real Go server on a fresh database of the `make test` Postgres container."""
import json
import os
import socket
import subprocess
import time
import urllib.request
from pathlib import Path

import pytest

SERVER_SRC = Path(__file__).resolve().parents[3] / "server"
PG_CONTAINER = os.environ.get("RUNYARD_TEST_PG_CONTAINER", "runyard-test-pg")


def _free_port():
    with socket.socket() as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]


@pytest.fixture(scope="session")
def server_bin(tmp_path_factory):
    if not os.environ.get("RUNYARD_TEST_DATABASE_URL"):
        pytest.skip("RUNYARD_TEST_DATABASE_URL not set; run `make test`")
    out = tmp_path_factory.mktemp("bin") / "runyard-server"
    subprocess.run(["go", "build", "-o", str(out), "."], cwd=SERVER_SRC, check=True)
    return out


@pytest.fixture
def server(server_bin, tmp_path, monkeypatch):
    """Starts the server on an empty database; yields its URL. RUNYARD_* and the cache point at it and tmp_path."""
    db = os.environ.get("RUNYARD_PYTEST_DB", "runyard_pytest")  # separate names let test runs share the container
    psql = ["docker", "exec", PG_CONTAINER, "psql", "-U", "postgres", "-q", "-c"]
    subprocess.run(psql + [f"DROP DATABASE IF EXISTS {db} WITH (FORCE)"], check=True)
    subprocess.run(psql + [f"CREATE DATABASE {db}"], check=True)
    base = os.environ["RUNYARD_TEST_DATABASE_URL"].rsplit("/", 1)[0]
    port = _free_port()
    env = dict(os.environ, DATABASE_URL=f"{base}/{db}?sslmode=disable", MEDIA_DIR=str(tmp_path / "media"), ADDR=f"127.0.0.1:{port}")
    key = subprocess.run([server_bin, "key", "create", "pytest"], env=env, capture_output=True, text=True, check=True).stdout.strip()
    proc = subprocess.Popen([server_bin], env=env, stderr=subprocess.DEVNULL)
    url = f"http://127.0.0.1:{port}"
    for _ in range(100):
        try:
            urllib.request.urlopen(url + "/api/v1/health", timeout=1)
            break
        except OSError:
            time.sleep(0.1)
    monkeypatch.setenv("RUNYARD_SERVER", url)
    monkeypatch.setenv("RUNYARD_API_KEY", key)
    monkeypatch.setattr("runyard.core.CACHE", tmp_path / "cache")
    yield url
    proc.kill()
    proc.wait()


def get(url):
    with urllib.request.urlopen(url, timeout=5) as r:
        return json.loads(r.read())
