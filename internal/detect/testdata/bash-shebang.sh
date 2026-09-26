#!/usr/bin/env bash
set -euo pipefail
readonly TARGET="${1:-localhost}"
echo "pinging ${TARGET}"
