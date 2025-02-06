#!/bin/sh

set -e

cd "$(dirname "$0")"
. ../util.sh
current_test=$(basename $(pwd))

export LOCAL_DIR=$(mktemp -d)

docker compose up -d --quiet-pull
sleep 5

docker compose exec backup backup

sleep 20

if [ $(ls -1 $LOCAL_DIR | wc -l) != "1" ]; then
  fail "Unexpected number of backups after initial run"
fi
pass "Found 1 backup files."

docker compose exec backup backup

if [ $(ls -1 $LOCAL_DIR | wc -l) != "1" ]; then
  fail "Unexpected number of backups after initial run"
fi
pass "Found 1 backup files."
