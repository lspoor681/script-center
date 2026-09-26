"""A script whose arguments are built at run time."""
import argparse

FLAGS = ["--alpha", "--beta"]
EXTRA = {"help": "computed"}


def make():
    parser = argparse.ArgumentParser()
    parser.add_argument(*FLAGS, **EXTRA)
    parser.add_argument("--port", type=int, default=8000, help="A known port.")
    sub = parser.add_subparsers(dest="command")
    deploy = sub.add_parser("deploy")
    deploy.add_argument("--region", required=True)
    return parser
