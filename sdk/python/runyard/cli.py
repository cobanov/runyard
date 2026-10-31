"""runyard command line. Every read command takes --json for scripts and agents.

    runyard runs ls [--project P] [--status S]
    runyard runs show RUN            RUN: id, id prefix or run name
    runyard logs RUN [-n 100] [-f]
    runyard samples RUN [--out DIR]  downloads the latest samples
    runyard sync                     sends runs saved while the server was unreachable
    runyard watch DIR [--project P] [--once] [--interval 10]
                                     sends TensorBoard event files under DIR, one run per directory, until Ctrl-C;
                                     --once reads them once and exits (needs the tensorboard package)
"""
import argparse
import json
import os
import signal
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


def api(path):
    return request("GET", server() + "/api/v1" + path)


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
        urllib.request.urlretrieve(server() + m["url"], dst)
        m["file"] = str(dst)
        if not a.json:
            print(dst)
    if a.json:
        dump(media)


def cmd_sync(a):
    """Sends what runs saved at exit while their server was unreachable."""
    from . import core

    files = sorted((core.CACHE / "pending").glob("*.json"))
    left = [f for f in files if not sync(f)]
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

    sub.add_parser("sync", help=cmd_sync.__doc__).set_defaults(fn=cmd_sync)

    watch = sub.add_parser("watch", help=cmd_watch.__doc__)
    watch.add_argument("dir")
    watch.add_argument("--project", help="default: RUNYARD_PROJECT, else the directory's name")
    watch.add_argument("--once", action="store_true", help="read everything once, finish the runs and exit (imports)")
    watch.add_argument("--interval", type=float, default=10, help="seconds between polls (default 10)")
    watch.set_defaults(fn=cmd_watch)
    a = p.parse_args(argv)
    try:
        return a.fn(a) or 0
    except (OSError, HTTPError) as e:  # unreachable server, or it refused the request
        print(f"runyard: {e}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
