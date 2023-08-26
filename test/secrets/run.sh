#!/bin/sh

set -e

cd $(dirname $0)
. ../util.sh
current_test=$(basename $(pwd))

docker swarm init

printf "test" | docker secret create minio_root_user -
printf "GMusLtUmILge2by+z890kQ" | docker secret create minio_root_password -

docker stack deploy --compose-file=docker-compose.yml test_stack

while [ -z $(docker ps -q -f name=backup) ]; do
  info "Backup container not ready yet. Retrying."
  sleep 1
done

sleep 20

docker exec $(docker ps -q -f name=backup) backup

docker run --rm \
  -v backup_data:/data alpine \
  ash -c 'tar -xf /data/backup/test.tar.gz && test -f /backup/pg_data/PG_VERSION'

pass "Found relevant files in untared backup."

sleep 5
expect_running_containers "5"

docker exec -e AWS_ACCESS_KEY_ID=test $(docker ps -q -f name=backup) backup \
  && fail "Backup should have failed due to duplicate env variables."

pass "Backup failed due to duplicate env variables."

docker exec -e AWS_ACCESS_KEY_ID_FILE=/tmp/nonexistant $(docker ps -q -f name=backup) backup \
  && fail "Backup should have failed due to non existing file env variable."

pass "Backup failed due to non existing file env variable."
