#!/bin/sh

set -e

cd "$(dirname "$0")"
. ../util.sh
current_test=$(basename $(pwd))

ssh-keygen -t rsa -m pem -b 4096 -N "test1234" -f id_rsa -C "docker-volume-backup@local"

docker compose up -d --quiet-pull
sleep 5

docker compose exec backup backup

sleep 5

expect_running_containers 3

docker run --rm \
  -v ssh_backup_data:/ssh_data \
  alpine \
  ash -c 'tar -xvf /ssh_data/test-hostnametoken.tar.gz -C /tmp && test -f /tmp/backup/app_data/offen.db'

pass "Found relevant files in decrypted and untared remote backups."

# The second part of this test checks if backups get deleted when the retention
# is set to 0 days (which it should not as it would mean all backups get deleted)
BACKUP_RETENTION_DAYS="0" docker compose up -d
sleep 5

docker compose exec backup backup

docker run --rm \
  -v ssh_backup_data:/ssh_data \
  alpine \
  ash -c '[ $(find /ssh_data/ -type f | wc -l) = "1" ]'

pass "Remote backups have not been deleted."

# The third part of this test checks if old backups get deleted when the retention
# is set to 7 days (which it should)

BACKUP_RETENTION_DAYS="7" docker compose up -d
sleep 5

echo "## Create first backup with no prune"
docker compose exec backup backup

# Set the modification date of the old backup to 14 days ago
docker run --rm \
  -v ssh_backup_data:/ssh_data \
  --user 1000 \
  alpine \
  ash -c 'touch -d@$(( $(date +%s) - 1209600 )) /ssh_data/test-hostnametoken-old.tar.gz'

echo "## Create second backup and prune"
docker compose exec backup backup

docker run --rm \
  -v ssh_backup_data:/ssh_data \
  alpine \
  ash -c 'test ! -f /ssh_data/test-hostnametoken-old.tar.gz && test -f /ssh_data/test-hostnametoken.tar.gz'

pass "Old remote backup has been pruned, new one is still present."

docker compose down --volumes
rm -f id_rsa id_rsa.pub
