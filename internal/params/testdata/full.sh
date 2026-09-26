#!/usr/bin/env bash
# Rebuilds the search index on a server.
set -euo pipefail

usage() {
    cat <<'EOF'
Usage: rebuild.sh [options] <server>

  -s, --server SERVER   the server to rebuild
  -m, --mode MODE       how much work to do
  -v, --verbose         chatty output
  -c, --config FILE     a config file
  -n, --dry-run         change nothing
  -h, --help            show this help

The server may also be given as the first argument.
EOF
}

SERVER=""
MODE="fast"
VERBOSE=0
CONFIG=""
DRY_RUN=0

while [[ $# -gt 0 ]]; do
    case "$1" in
        -s|--server)   SERVER="$2"; shift 2 ;;
        -m|--mode)     MODE="$2"; shift ;;
        --config=*)    CONFIG="${1#*=}"; shift ;;
        -c)            CONFIG="$2"; shift ;;
        -v|--verbose)  VERBOSE=1; shift ;;
        -n|--dry-run)  DRY_RUN=1; shift ;;
        -h|--help)     usage; exit 0 ;;
        *)             echo "unknown option: $1" >&2; usage >&2; exit 2 ;;
    esac
done

if [[ -z "$SERVER" ]]; then
    SERVER="${1:-}"
fi

echo "rebuilding $SERVER in $MODE"
