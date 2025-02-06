#!/bin/sh

set -e

cd "$(dirname "$0")"
. ../util.sh
current_test=$(basename $(pwd))

docker compose up -d --quiet-pull
sleep 5

docker compose exec backup backup

sleep 5

expect_running_containers "3"

docker run --rm \
  -v webdav_backup_data:/webdav_data \
  alpine \
  ash -c 'tar -xvf /webdav_data/data/my/new/path/test-hostnametoken.tar.gz -C /tmp && test -f /tmp/backup/app_data/offen.db'

pass "Found relevant files in untared remote backup."

# The second part of this test checks if backups get deleted when the retention
# is set to 0 days (which it should not as it would mean all backups get deleted)
BACKUP_RETENTION_PERIOD="1s" docker compose up -d
sleep 5

docker compose exec backup backup

docker run --rm \
  -v webdav_backup_data:/webdav_data \
  alpine \
  ash -c '[ $(find /webdav_data/data/my/new/path/ -type f | wc -l) = "1" ]'

pass "Remote backups have not been deleted."

# The third part of this test checks if old backups get deleted when the retention
# is set to 7 days (which it should)

BACKUP_RETENTION_PERIOD="168h" docker compose up -d
sleep 5

info "Create first backup with no prune"
docker compose exec backup backup

# Set the modification date of the old backup to 14 days ago
docker run --rm \
  -v webdav_backup_data:/webdav_data \
  --user 82 \
  alpine \
  ash -c 'touch -d@$(( $(date +%s) - 1209600 )) /webdav_data/data/my/new/path/test-hostnametoken-old.tar.gz'

info "Create second backup and prune"
docker compose exec backup backup

docker run --rm \
  -v webdav_backup_data:/webdav_data \
  alpine \
  ash -c 'test ! -f /webdav_data/data/my/new/path/test-hostnametoken-old.tar.gz && test -f /webdav_data/data/my/new/path/test-hostnametoken.tar.gz'

pass "Old remote backup has been pruned, new one is still present."
