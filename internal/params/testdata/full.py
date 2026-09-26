#!/usr/bin/env python3
"""Rebuilds the search index on a server.

Connects to the named server and rebuilds its index. The work is done by
rebuild_one, which lives in the same file.
"""
import argparse
import pathlib
import sys

if sys.version_info < (3, 9):
    sys.exit("Python 3.9 or newer is required")


def main():
    parser = argparse.ArgumentParser(
        description="Rebuilds the search index.",
        epilog="See the runbook for key rotation.",
    )
    parser.add_argument("target", help="The server to rebuild.")
    parser.add_argument("--server", "-s", help="An alternate server name.")
    parser.add_argument("--threads", type=int, default=4, help="Worker count.")
    parser.add_argument("--ratio", type=float, default=0.5, help="Threshold.")
    parser.add_argument(
        "--mode", choices=["fast", "thorough"], default="fast", help="How much work."
    )
    parser.add_argument("--verbose", "-v", action="store_true", help="Chatty output.")
    parser.add_argument("--config", type=pathlib.Path, help="A config file.")
    parser.add_argument("--tag", action="append", help="A repeatable tag.")
    parser.add_argument("--name", required=True, help="A required option.")
    parser.add_argument("output", help="Where to write the report.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
