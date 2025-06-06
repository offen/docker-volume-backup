#!/bin/sh

set -e

cd $(dirname $0)
. ../util.sh
current_test=$(basename $(pwd))

export LOCAL_DIR=$(mktemp -d)

docker stack deploy --compose-file=docker-compose.yml test_stack

while [ -z $(docker ps -q -f name=backup) ]; do
  info "Backup container not ready yet. Retrying."
  sleep 1
done

sleep 20

set +e
docker exec $(docker ps -q -f name=backup) backup
if [ $? = "0" ]; then
  fail "Expected script to exit with error code."
fi

if [ -f "${LOCAL_DIR}/test.tar.gz" ]; then
  fail "Found backup file that should not have been created."
fi

expect_running_containers "3"

pass "Script did not perform backup as there was a label collision."
