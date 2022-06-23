#!/bin/sh

set -e

cd "$(dirname "$0")"

mkdir -p local

docker-compose up -d
sleep 5

# A symlink for a known file in the volume is created so the test can check
# whether symlinks are preserved on backup.
docker-compose exec offen ln -s /var/opt/offen/offen.db /var/opt/offen/db.link
docker-compose exec backup backup

sleep 5
if [ "$(docker-compose ps -q | wc -l)" != "2" ]; then
  echo "[TEST:FAIL] Expected all containers to be running post backup, instead seen:"
  docker-compose ps
  exit 1
fi
echo "[TEST:PASS] All containers running post backup."

tar -xf ./local/test-hostnametoken.tar.gz -C /tmp && test -f /tmp/backup/app_data/offen.db
rm ./local/test-hostnametoken.tar.gz
test -L /tmp/backup/app_data/db.link

echo "[TEST:PASS] Found relevant files in decrypted and untared local backup."

test -L ./local/test-hostnametoken.latest.tar.gz
echo "[TEST:PASS] Found symlink to latest version in local backup."

# The second part of this test checks if backups get deleted when the retention
# is set to 0 days (which it should not as it would mean all backups get deleted)
# TODO: find out if we can test actual deletion without having to wait for a day
BACKUP_RETENTION_DAYS="0" docker-compose up -d
sleep 5

docker-compose exec backup backup

if [ "$(find ./local -type f | wc -l)" != "1" ]; then
  echo "[TEST:FAIL] Backups should not have been deleted, instead seen:"
  find ./local -type f
  exit 1
fi
echo "[TEST:PASS] Local backups have not been deleted."

docker-compose down --volumes
