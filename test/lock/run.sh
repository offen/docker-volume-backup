#!/bin/sh

set -e

cd "$(dirname "$0")"
. ../util.sh
current_test=$(basename $(pwd))

export LOCAL_DIR=$(mktemp -d)

docker compose up -d --quiet-pull
sleep 5

ec=0

docker compose exec -e BACKUP_RETENTION_DAYS=7 -e BACKUP_FILENAME=test.tar.gz backup backup & \
  { set +e; sleep 0.1; docker compose exec -e BACKUP_FILENAME=test2.tar.gz -e LOCK_TIMEOUT=1s backup backup; ec=$?;}

if [ "$ec" = "0" ]; then
  fail "Subsequent invocation exited 0"
fi
pass "Subsequent invocation did not exit 0"

sleep 5

if [ ! -f "${LOCAL_DIR}/test.tar.gz" ]; then
  fail "Could not find expected tar file"
fi
pass "Found expected tar file"

if [ -f "${LOCAL_DIR}/test2.tar.gz" ]; then
  fail "Subsequent invocation was expected to fail but created archive"
fi
pass "Subsequent invocation did not create archive"
