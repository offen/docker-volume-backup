#!/bin/sh

set -e

cd "$(dirname "$0")"

docker-compose up -d
sleep 5

docker-compose exec backup backup

sleep 5
if [ "$(docker-compose ps -q | wc -l)" != "3" ]; then
  echo "[TEST:FAIL] Expected all containers to be running post backup, instead seen:"
  docker-compose ps
  exit 1
fi
echo "[TEST:PASS] All containers running post backup."


docker run --rm -it \
  -v compose_webdav_backup_data:/webdav_data \
  alpine \
  ash -c 'tar -xvf /webdav_data/data/my/new/path/test-hostnametoken.tar.gz -C /tmp && test -f /tmp/backup/app_data/offen.db && \'

echo "[TEST:PASS] Found relevant files in untared remote backup."

# The second part of this test checks if backups get deleted when the retention
# is set to 0 days (which it should not as it would mean all backups get deleted)
# TODO: find out if we can test actual deletion without having to wait for a day
BACKUP_RETENTION_DAYS="0" docker-compose up -d
sleep 5

docker-compose exec backup backup

docker run --rm -it \
  -v compose_webdav_backup_data:/webdav_data \
  alpine \
  ash -c '[ $(find /webdav_data/data/my/new/path/ -type f | wc -l) = "1" ]'

echo "[TEST:PASS] Remote backups have not been deleted."

docker-compose down --volumes
