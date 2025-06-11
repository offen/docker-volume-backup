#!/bin/sh

set -e

cd $(dirname $0)
. ../util.sh
current_test=$(basename $(pwd))

export LOCAL_DIR=$(mktemp -d)
export TMP_DIR=$(mktemp -d)

docker swarm init

docker stack deploy --compose-file=docker-compose.yml test_stack

while [ -z $(docker ps -q -f name=backup) ]; do
  info "Backup container not ready yet. Retrying."
  sleep 1
done

sleep 20

docker exec $(docker ps -q -f name=backup) backup

tar -xvf "$LOCAL_DIR/test.tar.gz" -C $TMP_DIR
if [ ! -f "$TMP_DIR/backup/data/dump.sql" ]; then
  fail "Could not find file written by pre command."
fi
pass "Found expected file."

if [ -f "$TMP_DIR/backup/data/post.txt" ]; then
  fail "File created in post command was present in backup."
fi
pass "Did not find unexpected file."
