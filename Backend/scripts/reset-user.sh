#!/usr/bin/env bash
# Puts accounts back to the seeds' baseline on the live database, so people can practice making
# sidequests and wipe: their history (past sidequests, ratings, interests), their class calendar,
# their default upcoming sidequests (made again with fresh dates) and the showcase world as seeded.
# Everything else they have in plans goes; their account, friendships, direct messages and payment
# methods stay. A dry run that lists every item unless you pass --apply.
#
# Usage: Backend/scripts/reset-user.sh @handle[,@handle…] [--apply] [--allow-demo]
set -euo pipefail
if [[ $# -lt 1 || "$1" == -* ]]; then
  echo "usage: $0 @handle[,@handle…] [--apply] [--allow-demo]" >&2
  exit 2
fi
who="$1"
shift
exec "$(dirname "$0")/seed-live.sh" --reset "$who" "$@"
