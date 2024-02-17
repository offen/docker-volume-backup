#!/bin/sh

set -e

cd "$(dirname "$0")"
. ../util.sh
current_test=$(basename $(pwd))

export LOCAL_DIR=$(mktemp -d)

docker compose up -d --quiet-pull
sleep 5

docker compose exec backup backup

sleep 5

expect_running_containers "3"

if [ ! -f "$LOCAL_DIR/backup/test.tar.gz" ]; then
  fail "Could not find archive."
fi
pass "Archive was created."

docker compose logs backup
