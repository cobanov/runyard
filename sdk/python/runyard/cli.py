"""runyard command line. Every read command takes --json for scripts and agents.

    runyard runs ls [--project P] [--status S]
    runyard runs show RUN            RUN: id, id prefix or run name
    runyard logs RUN [-n 100] [-f]
    runyard samples RUN [--out DIR]  downloads the latest samples
    runyard artifacts RUN [--get NAME]  checkpoints and files; --get downloads an uploaded one
    runyard sync [--server URL]      sends runs saved while the server was unreachable (the next run does it too)
    runyard watch DIR [--project P] [--once] [--interval 10]
                                     sends TensorBoard event files under DIR, one run per directory, until Ctrl-C;
                                     --once reads them once and exits (needs the tensorboard package)
    runyard beacon [--name NAME] [--interval 10]
                                     sends this machine's GPU, CPU, memory and disk to the server until stopped,
                                     for the Machines page (run it as a service, see docs/server.md)
    runyard hooks install claude|codex|gemini|opencode [--project] [--remove]
                                     hooks that tell the server what a coding agent is doing (working, waiting
                                     for you, done) for its card on the kiosk page (see docs/agents.md);
                                     --project: this repository only, the better choice (claude)
"""
import argparse
import json
import os
import signal
import socket
import sys
import time
import urllib.parse
import urllib.request
from datetime import datetime, timezone
from pathlib import Path

from . import __version__
from .client import HTTPError, request
from .core import sync


def server():
    return (os.environ.get("RUNYARD_SERVER") or "http://localhost:8080").rstrip("/")


def key():
    return os.environ.get("RUNYARD_API_KEY", "")  # reads need it only when the server runs with RUNYARD_AUTH


def api(path):
    return request("GET", server() + "/api/v1" + path, key())


def download(path, dst):
    req = urllib.request.Request(server() + path, headers={"Authorization": "Bearer " + key()} if key() else {})
    with urllib.request.urlopen(req, timeout=300) as r, open(dst, "wb") as f:
        while chunk := r.read(1 << 20):
            f.write(chunk)


def dump(v):
    print(json.dumps(v, indent=1))


def ago(ts):
    s = (datetime.now(timezone.utc) - datetime.fromisoformat(ts)).total_seconds()
    for unit, n in (("d", 86400), ("h", 3600), ("m", 60)):
        if s >= n:
            return f"{int(s // n)}{unit} ago"
    return f"{int(s)}s ago"


def cmd_runs_ls(a):
    q = urllib.parse.urlencode({k: v for k, v in (("project", a.project), ("status", a.status), ("limit", a.limit)) if v})
    runs = api("/runs?" + q)
    if a.json:
        return dump(runs)
    for r in runs:
        print(f"{r['id'][:8]}  {r['status']:<8}  {r['project']}/{r['name']}  started {ago(r['started_at'])}")


def cmd_runs_show(a):
    r = api("/runs/" + urllib.parse.quote(a.run))
    if a.json:
        return dump(r)
    print(f"{r['project']}/{r['name']}  {r['status']}  id {r['id']}")
    print(f"started {ago(r['started_at'])} on {r['host'] or '?'}" + (f", ended {ago(r['ended_at'])}" if r["ended_at"] else ""))
    for title, d in (("config", r["config"]), ("summary", r["summary"])):
        if d:
            print(f"\n{title}:")
            for k, v in d.items():
                print(f"  {k}: {v}")
    if r.get("metrics"):
        print("\nmetrics (last / min / max, last step):")
        for k, m in sorted(r["metrics"].items()):
            print(f"  {k}: {m['last']} / {m['min']} / {m['max']}, step {m['step']}")


def cmd_logs(a):
    r = api("/runs/" + urllib.parse.quote(a.run))
    lines = api(f"/runs/{r['id']}/logs?tail&limit={a.n}")
    after = 0
    while True:
        for line in lines:
            print(json.dumps(line) if a.json else line["line"], flush=True)
        if not a.follow:
            return
        if not lines and api("/runs/" + r["id"])["status"] != "running":
            return
        after = lines[-1]["seq"] if lines else after
        time.sleep(2)
        lines = api(f"/runs/{r['id']}/logs?after={after}&limit=1000")


def cmd_samples(a):
    r = api("/runs/" + urllib.parse.quote(a.run))
    media = api(f"/runs/{r['id']}/media" + (f"?key={urllib.parse.quote(a.key)}" if a.key else ""))
    latest = {}
    for m in media:
        latest[m["key"]] = max(latest.get(m["key"], m["step"]), m["step"])
    media = [m for m in media if m["step"] == latest[m["key"]]]
    out = Path(a.out or f"runyard-samples/{r['name']}")
    out.mkdir(parents=True, exist_ok=True)
    for m in media:
        dst = out / f"{m['key'].replace('/', '_')}_{m['step']}_{m['id'][:8]}{Path(m['url']).suffix}"
        download(m["url"], dst)
        m["file"] = str(dst)
        if not a.json:
            print(dst)
    if a.json:
        dump(media)


def cmd_artifacts(a):
    r = api("/runs/" + urllib.parse.quote(a.run))
    arts = api(f"/runs/{r['id']}/artifacts")
    if a.get:
        arts = [x for x in arts if a.get in (x["id"], x["name"]) and x.get("url")]
        if not arts:
            print(f"runyard: no uploaded artifact {a.get!r} in {r['name']}", file=sys.stderr)
            return 1
        out = Path(a.out or ".")
        out.mkdir(parents=True, exist_ok=True)
        dst = out / Path(arts[0]["name"]).name
        download(arts[0]["url"], dst)
        return print(dst)
    if a.json:
        return dump(arts)
    for x in arts:
        size = f"{x['size'] / 1e6:.1f} MB" if x.get("size") is not None else "?"
        where = "uploaded" if x.get("url") else x["uri"]
        print(f"{x['kind']:<10}  {x['name']}  step {x['step']}  {size}  {where}")


def cmd_sync(a):
    """Sends what runs saved at exit while their server was unreachable."""
    from . import core

    files = sorted((core.CACHE / "pending").glob("*.json"))
    left = [f for f in files if not sync(f, server=a.server)]
    print(f"sent {len(files) - len(left)} of {len(files)} pending runs")
    return 1 if left else 0


def cmd_watch(a):
    """Sends TensorBoard event files under DIR, one run per directory; polls for new events until Ctrl-C."""
    if not Path(a.dir).is_dir():
        print(f"runyard: {a.dir} is not a directory", file=sys.stderr)
        return 1
    try:
        from .watch import watch
    except ImportError as e:
        print(f"runyard: watch needs tensorboard, pip install 'runyard[tensorboard]' ({e})", file=sys.stderr)
        return 1
    if not a.once:
        signal.signal(signal.SIGTERM, signal.default_int_handler)  # stop like Ctrl-C, so the runs end finished
    watch(a.dir, a.project, a.once, a.interval)


def cmd_beacon(a):
    """Sends this machine's GPU, CPU, memory and disk to the server every few seconds, until stopped."""
    if not key():
        print("runyard: beacon needs RUNYARD_API_KEY, a key from `runyard-server key create`", file=sys.stderr)
        return 1
    from .beacon import beacon

    signal.signal(signal.SIGTERM, signal.default_int_handler)  # systemd and launchd stop it like Ctrl-C
    try:
        beacon(server(), key(), a.name, a.interval, a.once, a.disk)
    except KeyboardInterrupt:
        pass


# The hook events each CLI sends; the server turns them into working, waiting, idle, error and ended.
HOOK_EVENTS = {
    "claude": ("UserPromptSubmit", "PostToolUse", "PermissionRequest", "Notification", "Stop", "StopFailure", "SessionEnd"),  # its http hooks skip SessionStart
    "codex": ("SessionStart", "UserPromptSubmit", "PostToolUse", "PermissionRequest", "Stop", "SessionEnd"),
    "gemini": ("BeforeAgent", "AfterTool", "Notification", "AfterAgent", "SessionEnd"),
}
HOOK_URL = "/api/v1/agents/events"
# OpenCode has no hook settings: a plugin subscribes to its events, here named for the hooks the server knows.
OPENCODE_PLUGIN = """\
// Written by `runyard hooks install opencode`, removed by the same command with --remove.
const url = %s
const hooks = {
  "session.execution.started": "UserPromptSubmit",
  "permission.asked": "PermissionRequest",
  "permission.replied": "PostToolUse",
  "session.execution.succeeded": "Stop",
  "session.execution.interrupted": "Stop",
  "session.execution.failed": "StopFailure",
  "session.deleted": "SessionEnd",
}

export default {
  id: "runyard",
  setup: async (ctx) => {
    const events = ctx.event.subscribe()[Symbol.asyncIterator]()
    const said = new Map() // session -> its last message, sent when the turn ends
    const dirs = new Map() // session -> its directory: one service serves them all, and most events do not say which
    const listen = async () => {
      for (let e = await events.next(); !e.done; e = await events.next()) {
        const { type, data } = e.value
        if (type === "session.created") dirs.set(data.sessionID, data.location?.directory)
        if (type === "session.text.ended") said.set(data.sessionID, data.text)
        if (!hooks[type]) continue
        const body = { session_id: data.sessionID, hook_event_name: hooks[type], cwd: dirs.get(data.sessionID) }
        if (hooks[type].startsWith("Stop")) {
          body.last_assistant_message = said.get(data.sessionID)
          said.delete(data.sessionID)
        }
        fetch(url, {
          method: "POST",
          headers: { Authorization: `Bearer ${process.env.RUNYARD_API_KEY}`, "Content-Type": "application/json" },
          body: JSON.stringify(body),
          signal: AbortSignal.timeout(3000),
        }).catch(() => {}) // a server that is down never holds the agent up
      }
    }
    listen()
    return () => events.return?.()
  },
}
"""


def curl_hook(url, header=""):
    """sh -c, so the key comes from the environment whether or not the CLI runs the command in a shell."""
    return {"type": "command", "command": f"sh -c 'curl -s -o /dev/null -m 3 -H \"Authorization: Bearer $RUNYARD_API_KEY\" {header}"
            f"-H \"Content-Type: application/json\" --data-binary @- \"{url}\" || true'"}


def opencode_hooks(a, url):
    path = Path(os.environ.get("XDG_CONFIG_HOME") or Path.home() / ".config") / "opencode/plugin/runyard.js"
    if a.remove:
        path.unlink(missing_ok=True)
        return print(f"removed {path}")
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(OPENCODE_PLUGIN % json.dumps(url))
    print(f"{path}: posts to {url}\nrestart OpenCode's service to load it: opencode service restart")
    if not key():
        print("runyard: set RUNYARD_API_KEY where you start OpenCode, or the server refuses its events", file=sys.stderr)


def cmd_hooks(a):
    """Adds hooks that tell the server what a coding agent is doing, or with --remove takes them out."""
    url = f"{server()}{HOOK_URL}?tool={a.cli}"
    if a.project and a.cli != "claude":
        print(f"runyard: --project only works for claude so far; {a.cli}'s hooks are per user", file=sys.stderr)
        return 1
    if a.cli == "opencode":
        return opencode_hooks(a, url)
    if a.cli == "claude":  # an http hook: no script, and a failed one never holds Claude up
        path = Path(os.environ.get("CLAUDE_CONFIG_DIR") or Path.home() / ".claude") / "settings.json"
        if a.project:  # settings.local.json stays out of git: the hooks need this person's key, and a teammate has none
            path, url = Path(".claude/settings.local.json"), url + "&card=1"
        hook = {"type": "http", "url": url, "headers": {"Authorization": "Bearer $RUNYARD_API_KEY"},
                "allowedEnvVars": ["RUNYARD_API_KEY"], "timeout": 3}
    elif a.cli == "gemini":  # its session id reaches hooks but not runs, so hooks name the card runs use: machine and directory
        path = Path.home() / ".gemini/settings.json"
        hook = curl_hook(url, '-H "X-Runyard-Session: $(hostname):$GEMINI_CWD" ')
    else:
        path = Path(os.environ.get("CODEX_HOME") or Path.home() / ".codex") / "hooks.json"
        hook = curl_hook(url)
    try:
        cfg = json.loads(path.read_text()) if path.exists() else {}
    except ValueError as e:
        print(f"runyard: {path} is not JSON ({e}), left as it is", file=sys.stderr)
        return 1
    hooks = cfg.get("hooks", {})
    for event in list(hooks):  # take out runyard's own hooks first, so installing twice changes nothing
        hooks[event] = [g for g in hooks[event] if not any(HOOK_URL in h.get("url", "") + h.get("command", "") for h in g.get("hooks", []))]
        if not hooks[event]:
            del hooks[event]
    if not a.remove:
        for event in HOOK_EVENTS[a.cli]:
            hooks.setdefault(event, []).append({"hooks": [hook]})
    cfg["hooks"] = hooks
    if not hooks:
        del cfg["hooks"]
    if path.exists():
        path.with_name(path.name + ".bak").write_bytes(path.read_bytes())
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(cfg, indent=2) + "\n")
    if a.remove:
        return print(f"removed runyard's hooks from {path}")
    print(f"{path}: {len(HOOK_EVENTS[a.cli])} hooks post to {url}")
    if a.cli == "codex":
        print("trust them with /hooks in Codex: until then it skips them, and `codex exec` does not say so")
    if not key():
        print("runyard: set RUNYARD_API_KEY where you start the agent, or the server refuses its hooks", file=sys.stderr)


def main(argv=None):
    p = argparse.ArgumentParser(prog="runyard", description="Reads runs from the server in RUNYARD_SERVER; watch sends to it.")
    p.add_argument("--version", action="version", version=__version__)
    sub = p.add_subparsers(dest="cmd", required=True)
    js = argparse.ArgumentParser(add_help=False)
    js.add_argument("--json", action="store_true", help="print JSON")

    runs = sub.add_parser("runs", help="list or show runs").add_subparsers(dest="sub", required=True)
    ls = runs.add_parser("ls", parents=[js], help="list runs, newest first")
    ls.add_argument("--project")
    ls.add_argument("--status", choices=["running", "finished", "failed", "crashed"])
    ls.add_argument("--limit", type=int, default=30)
    ls.set_defaults(fn=cmd_runs_ls)
    show = runs.add_parser("show", parents=[js], help="config, summary and metrics of a run")
    show.add_argument("run", help="id, id prefix or run name")
    show.set_defaults(fn=cmd_runs_show)

    logs = sub.add_parser("logs", parents=[js], help="a run's output")
    logs.add_argument("run")
    logs.add_argument("-n", type=int, default=100, help="last N lines (default 100)")
    logs.add_argument("-f", "--follow", action="store_true", help="keep printing new lines while the run is running")
    logs.set_defaults(fn=cmd_logs)

    samples = sub.add_parser("samples", parents=[js], help="download a run's latest samples")
    samples.add_argument("run")
    samples.add_argument("--key")
    samples.add_argument("--out", help="directory (default runyard-samples/<run>)")
    samples.set_defaults(fn=cmd_samples)

    arts = sub.add_parser("artifacts", parents=[js], help="a run's checkpoints and files; --get downloads an uploaded one")
    arts.add_argument("run")
    arts.add_argument("--get", metavar="NAME", help="download the uploaded artifact with this name or id")
    arts.add_argument("--out", help="directory for --get (default: here)")
    arts.set_defaults(fn=cmd_artifacts)

    sy = sub.add_parser("sync", help=cmd_sync.__doc__)
    sy.add_argument("--server", help="send them here instead of the address each run was started with")
    sy.set_defaults(fn=cmd_sync)

    watch = sub.add_parser("watch", help=cmd_watch.__doc__)
    watch.add_argument("dir")
    watch.add_argument("--project", help="default: RUNYARD_PROJECT, else the directory's name")
    watch.add_argument("--once", action="store_true", help="read everything once, finish the runs and exit (imports)")
    watch.add_argument("--interval", type=float, default=10, help="seconds between polls (default 10)")
    watch.set_defaults(fn=cmd_watch)

    bc = sub.add_parser("beacon", help=cmd_beacon.__doc__)
    bc.add_argument("--name", default=socket.gethostname(), help="this machine's name (default: its hostname, as runs record it)")
    bc.add_argument("--interval", type=float, default=10, help="seconds between readings (default 10)")
    bc.add_argument("--disk", default="/", help="the filesystem whose usage to report (default /)")
    bc.add_argument("--once", action="store_true", help="send one reading and exit")
    bc.set_defaults(fn=cmd_beacon)

    hooks = sub.add_parser("hooks", help="hooks that tell the server what a coding agent is doing").add_subparsers(dest="sub", required=True)
    hi = hooks.add_parser("install", help=cmd_hooks.__doc__)
    hi.add_argument("cli", choices=[*sorted(HOOK_EVENTS), "opencode"])
    hi.add_argument("--project", action="store_true", help="for this repository only, so no other session reaches the server (claude)")
    hi.add_argument("--remove", action="store_true", help="take runyard's hooks out instead")
    hi.set_defaults(fn=cmd_hooks)

    argv = list(sys.argv[1:] if argv is None else argv)
    if argv[:1] == ["agent"]:  # the old name; services set up with it keep working
        # shortcut: an alias forever; drop it once no installed service runs `runyard agent`
        print("runyard: `runyard agent` is now `runyard beacon`", file=sys.stderr)
        argv[0] = "beacon"
    a = p.parse_args(argv)
    try:
        return a.fn(a) or 0
    except (OSError, HTTPError) as e:  # unreachable server, or it refused the request
        print(f"runyard: {e}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
