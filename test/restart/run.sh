#!/bin/sh

set -e

cd "$(dirname "$0")"
. ../util.sh
current_test=$(basename $(pwd))

export LOCAL_DIR=$(mktemp -d)

mkdir /test-backup-data && dd if=/dev/urandom of=/test-backup-data/testfile.img bs=1M count=3000

docker compose up -d --quiet-pull
sleep 5

# no lazy restart, both backups should run sequentially and stop the container
docker compose exec -T -e BACKUP_RETENTION_DAYS=7 -e BACKUP_FILENAME=test.tar.gz backup backup &
background_pid_1=$!
sleep 1
docker compose exec -T -e BACKUP_FILENAME=test2.tar.gz backup backup &
background_pid_2=$!

sleep 2
expect_running_containers 1
wait $background_pid_1
expect_running_containers 2
wait $background_pid_2
expect_running_containers 2

if [ ! -f "${LOCAL_DIR}/test.tar.gz" ] || [ ! -f "${LOCAL_DIR}/test2.tar.gz" ]; then
  fail "Could not find expected tar files"
fi
pass "Found expected tar files"

rm -rf "${LOCAL_DIR}/test.tar.gz" "${LOCAL_DIR}/test2.tar.gz"

pass "Container restart without lazy restart works as expected"

# lazy restart, both backups should run sequentially and keep the container stopped
docker compose exec -T -e BACKUP_RETENTION_DAYS=7 -e BACKUP_FILENAME=test.tar.gz -e ACTIVATE_LAZY_RESTART=true backup backup &
background_pid_1=$!
sleep 1
docker compose exec -T -e BACKUP_FILENAME=test2.tar.gz -e ACTIVATE_LAZY_RESTART=true backup backup &
background_pid_2=$!

sleep 2
expect_running_containers 1
wait $background_pid_1
expect_running_containers 1
wait $background_pid_2
expect_running_containers 2

if [ ! -f "${LOCAL_DIR}/test.tar.gz" ] || [ ! -f "${LOCAL_DIR}/test2.tar.gz" ]; then
  fail "Could not find expected tar files"
fi
pass "Found expected tar files"

rm -rf "${LOCAL_DIR}/test.tar.gz" "${LOCAL_DIR}/test2.tar.gz"

pass "Container restart with lazy restart works as expected"
