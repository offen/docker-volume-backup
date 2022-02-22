#!/bin/sh

set -e

cd $(dirname $0)


docker-compose up -d
sleep 30 # mariadb likes to take a bit before responding

docker-compose exec backup backup
sudo cp -r $(docker volume inspect --format='{{ .Mountpoint }}' commands_archive) ./local

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
sudo rm -rf ./local


echo "[TEST:INFO] Running commands test in swarm mode next."

docker swarm init

docker stack deploy --compose-file=docker-compose.yml test_stack

while [ -z $(docker ps -q -f name=backup) ]; do
  echo "[TEST:INFO] Backup container not ready yet. Retrying."
  sleep 1
done

sleep 20

docker exec $(docker ps -q -f name=backup) backup

sudo cp -r $(docker volume inspect --format='{{ .Mountpoint }}' test_stack_archive) ./local

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

docker stack rm test_stack
docker swarm leave --force
