#!/bin/sh

set -e

cd $(dirname $0)

docker swarm init

docker stack deploy --compose-file=docker-compose.yml test_stack

while [ -z $(docker ps -q -f name=backup) ]; do
  echo "[TEST:INFO] Backup container not ready yet. Retrying."
  sleep 1
done

sleep 20

docker exec $(docker ps -q -f name=backup) backup

docker run --rm -it \
  -v test_stack_backup_data:/data alpine \
  ash -c 'tar -xf /data/backup/test.tar.gz && test -f /backup/pg_data/PG_VERSION'

echo "[TEST:PASS] Found relevant files in untared backup."

if [ "$(docker ps -q | wc -l)" != "5" ]; then
  echo "[TEST:FAIL] Expected all containers to be running post backup, instead seen:"
  docker ps -a
  exit 1
fi

echo "[TEST:PASS] All containers running post backup."

docker stack rm test_stack

docker swarm leave --force
