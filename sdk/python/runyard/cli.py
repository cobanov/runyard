import argparse

from . import __version__


def main(argv=None):
    p = argparse.ArgumentParser(prog="runyard")
    p.add_argument("--version", action="version", version=__version__)
    p.parse_args(argv)
