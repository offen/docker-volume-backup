#!/bin/sh

set -e

cd $(dirname $0)

mkdir -p local

docker-compose up -d
sleep 5

docker-compose exec offen ln -s /var/opt/offen/offen.db /var/opt/offen/db.link
docker-compose exec backup backup

docker run --rm -it \
  -v compose_minio_backup_data:/minio_data \
  -v compose_webdav_backup_data:/webdav_data alpine \
  ash -c 'apk add gnupg && \
          echo 1234secret | gpg -d --pinentry-mode loopback --passphrase-fd 0 --yes /minio_data/backup/test-hostnametoken.tar.gz.gpg > /tmp/test-hostnametoken.tar.gz && tar -xf /tmp/test-hostnametoken.tar.gz -C /tmp && test -f /tmp/backup/app_data/offen.db && \
          echo 1234secret | gpg -d --pinentry-mode loopback --passphrase-fd 0 --yes /webdav_data/data/test-hostnametoken.tar.gz.gpg > /tmp/test-hostnametoken.tar.gz && tar -xf /tmp/test-hostnametoken.tar.gz -C /tmp && test -f /tmp/backup/app_data/offen.db'

echo "[TEST:PASS] Found relevant files in untared remote backups."

test -L ./local/test-hostnametoken.latest.tar.gz.gpg
echo 1234secret | gpg -d --yes --passphrase-fd 0 ./local/test-hostnametoken.tar.gz.gpg > ./local/decrypted.tar.gz
tar -xf ./local/decrypted.tar.gz -C /tmp && test -f /tmp/backup/app_data/offen.db
rm ./local/decrypted.tar.gz
test -L /tmp/backup/app_data/db.link

echo "[TEST:PASS] Found relevant files in untared local backup."

if [ "$(docker-compose ps -q | wc -l)" != "3" ]; then
  echo "[TEST:FAIL] Expected all containers to be running post backup, instead seen:"
  docker-compose ps
  exit 1
fi

echo "[TEST:PASS] All containers running post backup."

# The second part of this test checks if backups get deleted when the retention
# is set to 0 days (which it should not as it would mean all backups get deleted)
# TODO: find out if we can test actual deletion without having to wait for a day
BACKUP_RETENTION_DAYS="0" docker-compose up -d
sleep 5

docker-compose exec backup backup

docker run --rm -it \
  -v compose_minio_backup_data:/minio_data \
  -v compose_webdav_backup_data:/webdav_data alpine \
  ash -c '[ $(find /minio_data/backup/ -type f | wc -l) = "1" ] && \
          [ $(find /webdav_data/backup/ -type f | wc -l) = "1" ]'

echo "[TEST:PASS] Remote backups have not been deleted."

if [ "$(find ./local -type f | wc -l)" != "1" ]; then
  echo "[TEST:FAIL] Backups should not have been deleted, instead seen:"
  find ./local -type f
fi

echo "[TEST:PASS] Local backups have not been deleted."

docker-compose down --volumes
