#!/bin/sh

set -e

cd "$(dirname "$0")"
. ../util.sh
current_test=$(basename $(pwd))

docker compose up -d
sleep 5

# A symlink for a known file in the volume is created so the test can check
# whether symlinks are preserved on backup.
docker compose exec backup backup

sleep 5

expect_running_containers "3"

docker-compose run --rm az_cli \
  az storage blob download -f /dump/test.tar.gz -c test-container -n path/to/backup/test.tar.gz
tar -xvf ./local/test.tar.gz -C /tmp && test -f /tmp/backup/app_data/offen.db

pass "Found relevant files in untared remote backups."

# The second part of this test checks if backups get deleted when the retention
# is set to 0 days (which it should not as it would mean all backups get deleted)
# TODO: find out if we can test actual deletion without having to wait for a day
BACKUP_RETENTION_DAYS="0" docker-compose up -d
sleep 5

docker compose exec backup backup

docker compose run --rm az_cli \
  az storage blob download -f /dump/test.tar.gz -c test-container -n path/to/backup/test.tar.gz
test -f ./local/test.tar.gz

pass "Remote backups have not been deleted."

docker compose down --volumes
