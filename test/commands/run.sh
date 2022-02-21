#!/bin/sh

set -e

cd $(dirname $0)

mkdir -p local

docker-compose up -d
sleep 30 # mariadb likes to take a bit before responding

docker-compose exec backup backup

tar -xvf ./local/test.tar.gz
if [ ! -f ./backup/data/dump.sql ]; then
  echo "[TEST:FAIL] Could not find file written by pre command."
  exit 1
fi
echo "[TEST:PASS] Found expected file."

if [ -f ./backup/data/post.txt ]; then
  echo "[TEST:FAIL] File created in post command was present in backup."
  exit 1
fi
echo "[TEST:PASS] Did not find unexpected file."

docker-compose down --volumes
