#!/bin/sh

set -e

cd "$(dirname "$0")"
. ../util.sh
current_test=$(basename $(pwd))

export LOCAL_DIR=$(mktemp -d)
export TMP_DIR=$(mktemp -d)

download_az () {
  docker compose run --rm az_cli \
    az storage blob download -f /dump/$1.tar.gz -c test-container -n path/to/backup/$1.tar.gz
}

docker compose up -d --quiet-pull

sleep 5

docker compose exec backup backup

sleep 5

expect_running_containers "3"

download_az "test"

tar -xvf "$LOCAL_DIR/test.tar.gz" -C $TMP_DIR

if [ ! -f "$TMP_DIR/backup/app_data/offen.db" ]; then
  fail "Could not find expeced file in untared backup"
fi

pass "Found relevant files in untared remote backups."

# The second part of this test checks if backups get deleted when the retention
# is set to 0 days (which it should not as it would mean all backups get deleted)
BACKUP_RETENTION_DAYS="0" docker compose up -d
sleep 5

docker compose exec backup backup

download_az "test"
test -f "$LOCAL_DIR/test.tar.gz"
pass "Remote backups have not been deleted."

# The third part of this test checks if old backups get deleted when the retention
# is set to 7 days (which it should)

BACKUP_RETENTION_DAYS="7" docker compose up -d
sleep 5

info "Create first backup with no prune"
docker compose exec backup backup

sudo date --set="14 days ago"

docker compose run --rm az_cli \
    az storage blob upload -f /dump/test.tar.gz -c test-container -n path/to/backup/test-old.tar.gz

sudo date --set="14 days"

info "Create second backup and prune"
docker compose exec backup backup

info "Download first backup which should be pruned"
download_az "test-old" || true
test ! -f ./local/test-old.tar.gz
test -f ./local/test.tar.gz

pass "Old remote backup has been pruned, new one is still present."
