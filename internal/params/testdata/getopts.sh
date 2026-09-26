#!/bin/sh
# Rebuilds an index using getopts.
set -e
while getopts ":s:m:vh" opt; do
    case "$opt" in
        s) SERVER="$OPTARG" ;;
        m) MODE="$OPTARG" ;;
        v) VERBOSE=1 ;;
        h) usage; exit 0 ;;
    esac
done
shift $((OPTIND - 1))
echo "$SERVER $MODE $VERBOSE $*"
