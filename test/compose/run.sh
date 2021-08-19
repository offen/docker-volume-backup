#!/bin/sh

set -e

cd $(dirname $0)

mkdir -p local

docker-compose up -d
sleep 5

docker-compose exec backup backup

docker run --rm -it \
  -v compose_backup_data:/data alpine \
  ash -c 'tar -xf /data/backup/test.tar.gz && test -f /backup/app_data/offen.db'

echo "[TEST:PASS] Found relevant files in untared remote backup."

tar -xf ./local/test.tar.gz -C /tmp && test -f /tmp/backup/app_data/offen.db

echo "[TEST:PASS] Found relevant files in untared local backup."

if [ "$(docker-compose ps -q | wc -l)" != "3" ]; then
  echo "[TEST:FAIL] Expected all containers to be running post backup, instead seen:"
  docker-compose ps
  exit 1
fi

echo "[TEST:PASS] All containers running post backup."

docker-compose down --volumes
