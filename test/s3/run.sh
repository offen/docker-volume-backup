#!/bin/sh

set -e

cd "$(dirname "$0")"
. ../util.sh
current_test=$(basename $(pwd))

docker compose up -d --quiet-pull --wait

docker compose exec backup backup

sleep 5

expect_running_containers "3"

docker run --rm \
  -v minio_backup_data:/minio_data \
  alpine \
  ash -c 'tar -xvf /minio_data/backup/test-hostnametoken.tar.gz -C /tmp && test -f /tmp/backup/app_data/offen.db'

pass "Found relevant files in untared remote backups."

# The second part of this test checks if backups get deleted when the retention
# is set to 0 days (which it should not as it would mean all backups get deleted)
BACKUP_RETENTION_DAYS="0" docker compose up -d --wait

docker compose exec backup backup

docker run --rm \
  -v minio_backup_data:/minio_data \
  alpine \
  ash -c '[ $(find /minio_data/backup/ -type f | wc -l) = "1" ]'

pass "Remote backups have not been deleted."

# The third part of this test checks if old backups get deleted when the retention
# is set to 7 days (which it should)

BACKUP_RETENTION_DAYS="7" docker compose up -d --wait

info "Create first backup with no prune"
docker compose exec backup backup

docker run --rm \
  -v minio_backup_data:/minio_data \
  alpine \
  ash -c 'touch -d@$(( $(date +%s) - 1209600 )) /minio_data/backup/test-hostnametoken-old.tar.gz'

info "Create second backup and prune"
docker compose exec backup backup

docker run --rm \
  -v minio_backup_data:/minio_data \
  alpine \
  ash -c 'test ! -f /minio_data/backup/test-hostnametoken-old.tar.gz && test -f /minio_data/backup/test-hostnametoken.tar.gz'

pass "Old remote backup has been pruned, new one is still present."
