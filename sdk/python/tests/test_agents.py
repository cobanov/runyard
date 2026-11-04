"""Coding agents: a run names the agent session it was started from, and `runyard hooks install` wires up the CLIs."""
import json
import os

import pytest

import runyard as ry
from runyard.cli import main
from runyard.core import _agent
from conftest import get

AGENT_VARS = ("RUNYARD_AGENT", "CLAUDE_CODE_SESSION_ID", "CODEX_SESSION_ID", "OPENCODE_SESSION_ID", "GEMINI_CLI", "AI_AGENT")


@pytest.fixture
def no_agent(monkeypatch):
    for v in AGENT_VARS:  # the tests may themselves run inside an agent
        monkeypatch.delenv(v, raising=False)
    return monkeypatch


@pytest.mark.parametrize("env, tool, session", [
    ({}, None, None),
    ({"CLAUDE_CODE_SESSION_ID": "c1", "AI_AGENT": "claude-code_2-1-316_agent"}, "claude", "c1"),
    ({"CODEX_SESSION_ID": "x1"}, "codex", "x1"),
    ({"OPENCODE_SESSION_ID": "ses_1", "AI_AGENT": "opencode"}, "opencode", "ses_1"),
    ({"GEMINI_CLI": "1"}, "gemini", "host:cwd"),
    ({"AI_AGENT": "amp_1.2_agent"}, "amp", "host:cwd"),
    ({"RUNYARD_AGENT": "anil-sweep-1", "CLAUDE_CODE_SESSION_ID": "c1"}, "claude", "anil-sweep-1"),
    ({"RUNYARD_AGENT": "night"}, "agent", "night"),
])
def test_agent_from_env(no_agent, env, tool, session):
    for k, v in env.items():
        no_agent.setenv(k, v)
    no_agent.setattr("socket.gethostname", lambda: "host")
    no_agent.setattr("os.getcwd", lambda: "cwd")
    a = _agent()
    assert (a and a["tool"], a and a["session"]) == (tool, session)


def test_run_lands_on_agent_card(server, no_agent):
    no_agent.setenv("CLAUDE_CODE_SESSION_ID", "sess-1")
    run = ry.init(project="cards", name="a", capture_output=False, system=False)
    run.finish()
    agents = get(server + "/api/v1/agents")
    assert [(a["id"], a["state"], a["runs"]) for a in agents] == [("claude:sess-1", "done", 1)]
    assert get(server + f"/api/v1/runs/{run.id}")["agent"] == "claude:sess-1"


def test_hooks_install(tmp_path, no_agent, capsys):
    no_agent.setenv("RUNYARD_SERVER", "https://ry.example")
    no_agent.setenv("RUNYARD_API_KEY", "ry_x")
    no_agent.setenv("CLAUDE_CONFIG_DIR", str(tmp_path / "claude"))
    no_agent.setenv("CODEX_HOME", str(tmp_path / "codex"))
    settings = tmp_path / "claude" / "settings.json"
    settings.parent.mkdir()
    mine = {"type": "command", "command": "say done"}
    settings.write_text(json.dumps({"model": "opus", "hooks": {"Stop": [{"hooks": [mine]}]}}))

    assert main(["hooks", "install", "claude"]) == 0
    assert main(["hooks", "install", "claude"]) == 0  # twice changes nothing
    cfg = json.loads(settings.read_text())
    assert cfg["model"] == "opus" and cfg["hooks"]["Stop"][0]["hooks"] == [mine] and len(cfg["hooks"]["Stop"]) == 2
    http = cfg["hooks"]["PermissionRequest"][0]["hooks"][0]
    assert http["url"] == "https://ry.example/api/v1/agents/events?tool=claude"
    assert http["headers"]["Authorization"] == "Bearer $RUNYARD_API_KEY" and "ry_x" not in settings.read_text()
    assert main(["hooks", "install", "claude", "--remove"]) == 0
    assert json.loads(settings.read_text()) == {"model": "opus", "hooks": {"Stop": [{"hooks": [mine]}]}}

    assert main(["hooks", "install", "codex"]) == 0
    cmd = json.loads((tmp_path / "codex" / "hooks.json").read_text())["hooks"]["Stop"][0]["hooks"][0]["command"]
    assert cmd.startswith("sh -c 'curl") and "$RUNYARD_API_KEY" in cmd and "tool=codex" in cmd

    settings.write_text("{not json")
    assert main(["hooks", "install", "claude"]) == 1
    assert settings.read_text() == "{not json"

    no_agent.chdir(tmp_path)
    assert main(["hooks", "install", "claude", "--project"]) == 0
    local = tmp_path / ".claude" / "settings.local.json"
    assert json.loads(local.read_text())["hooks"]["Stop"][0]["hooks"][0]["url"].endswith("?tool=claude&card=1")
    assert main(["hooks", "install", "claude", "--project", "--remove"]) == 0
    assert json.loads(local.read_text()) == {}
    assert main(["hooks", "install", "codex", "--project"]) == 1


def test_codex_hook_command_posts(server, tmp_path, no_agent):
    """The command Codex runs reaches the server with the event on stdin."""
    no_agent.setenv("CODEX_HOME", str(tmp_path))
    no_agent.setenv("CODEX_SESSION_ID", "cx")
    ry.init(project="cards", name="a", capture_output=False, system=False).finish()  # a card starts with a run
    main(["hooks", "install", "codex"])
    cmd = json.loads((tmp_path / "hooks.json").read_text())["hooks"]["UserPromptSubmit"][0]["hooks"][0]["command"]
    import subprocess

    event = {"session_id": "cx", "hook_event_name": "UserPromptSubmit", "cwd": "/repo"}
    subprocess.run(cmd, shell=True, input=json.dumps(event), text=True, check=True, env=dict(os.environ))
    assert [(a["id"], a["state"]) for a in get(server + "/api/v1/agents")] == [("codex:cx", "working")]
