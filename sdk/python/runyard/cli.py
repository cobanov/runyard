"""runyard command line."""
import argparse
import sys

from . import __version__
from .core import sync


def cmd_sync(args):
    """Sends what runs saved at exit while their server was unreachable."""
    from . import core

    files = sorted((core.CACHE / "pending").glob("*.json"))
    left = [f for f in files if not sync(f)]
    print(f"sent {len(files) - len(left)} of {len(files)} pending runs")
    return 1 if left else 0


def main(argv=None):
    p = argparse.ArgumentParser(prog="runyard")
    p.add_argument("--version", action="version", version=__version__)
    sub = p.add_subparsers(dest="cmd", required=True)
    sub.add_parser("sync", help=cmd_sync.__doc__).set_defaults(fn=cmd_sync)
    args = p.parse_args(argv)
    return args.fn(args)


if __name__ == "__main__":
    sys.exit(main())
