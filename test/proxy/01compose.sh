#!/bin/sh

set -e

cd $(dirname $0)
. ../util.sh
current_test=$(basename $(pwd))

export LOCAL_DIR=$(mktemp -d)

docker compose up -d --quiet-pull --wait

# The default configuration in docker-compose.yml should
# successfully create a backup.
docker compose exec backup backup

sleep 5

expect_running_containers "3"

if [ ! -f "$LOCAL_DIR/test.tar.gz" ]; then
  fail "Archive was not created"
fi
pass "Found relevant archive file."

# Disabling POST should make the backup run fail
ALLOW_POST="0" docker compose up -d --wait

set +e
docker compose exec backup backup
if [ $? = "0" ]; then
  fail "Expected invocation to exit non-zero."
fi
set -e
pass "Invocation exited non-zero."

docker compose down --volumes
