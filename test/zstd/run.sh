#!/bin/sh

set -e

cd $(dirname $0)
. ../util.sh
current_test=$(basename $(pwd))

docker network create test_network
docker volume create app_data

mkdir -p local

docker run -d \
  --name offen \
  --network test_network \
  -v app_data:/var/opt/offen/ \
  offen/offen:latest

sleep 10

docker run --rm \
  --network test_network \
  -v app_data:/backup/app_data \
  -v ./local:/archive \
  -v /var/run/docker.sock:/var/run/docker.sock \
  --env BACKUP_COMPRESSION=zst \
  --env BACKUP_FILENAME='test.{{ .Extension }}' \
  --entrypoint backup \
  offen/docker-volume-backup:${TEST_VERSION:-canary}

tmp_dir=$(mktemp -d)
tar -xvf ./local/test.tar.zst --zstd -C $tmp_dir
if [ ! -f "$tmp_dir/backup/app_data/offen.db" ]; then
  fail "Could not find expected file in untared archive."
fi
pass "Found relevant files in untared local backup."

# This test does not stop containers during backup. This is happening on
# purpose in order to cover this setup as well.
expect_running_containers "1"

docker rm $(docker stop offen)

docker volume rm app_data
docker network rm test_network
