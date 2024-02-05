#!/bin/sh

set -e

cd $(dirname $0)
. ../util.sh
current_test=$(basename $(pwd))

export LOCAL_DIR=$(mktemp -d)

docker compose up -d --quiet-pull
sleep 5

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
ALLOW_POST="0" docker compose up -d
sleep 5

set +e
docker compose exec backup backup
if [ $? = "0" ]; then
  fail "Expected invocation to exit non-zero."
fi
set -e
pass "Invocation exited non-zero."

docker compose down --volumes

# Next, the test is run against a Swarm setup

docker swarm init

export LOCAL_DIR=$(mktemp -d)

docker stack deploy --compose-file=docker-compose.swarm.yml test_stack

sleep 20

# The default configuration in docker-compose.swarm.yml should
# successfully create a backup in Swarm mode.
docker exec $(docker ps -q -f name=backup) backup

if [ ! -f "$LOCAL_DIR/test.tar.gz" ]; then
  fail "Archive was not created"
fi

pass "Found relevant archive file."

sleep 5
expect_running_containers "3"

# Disabling POST should make the backup run fail
ALLOW_POST="0" docker stack deploy --compose-file=docker-compose.swarm.yml test_stack

sleep 20

set +e
docker exec $(docker ps -q -f name=backup) backup
if [ $? = "0" ]; then
  fail "Expected invocation to exit non-zero."
fi
set -e

pass "Invocation exited non-zero."
