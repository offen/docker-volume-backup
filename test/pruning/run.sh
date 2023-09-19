#!/bin/sh

# Tests prune-skipping with multiple backends (local, s3)
# Pruning itself is tested individually for each storage backend

set -e

cd "$(dirname "$0")"
. ../util.sh
current_test=$(basename $(pwd))

export LOCAL_DIR=$(mktemp -d)

docker compose up -d --quiet-pull
sleep 5

docker compose exec backup backup

sleep 5

expect_running_containers "3"

touch -r "$LOCAL_DIR/test-hostnametoken.tar.gz" -d "14 days ago" "$LOCAL_DIR/test-hostnametoken-old.tar.gz"

docker run --rm \
  -v minio_backup_data:/minio_data \
  alpine \
  ash -c 'touch -d@$(( $(date +%s) - 1209600 )) /minio_data/backup/test-hostnametoken-old.tar.gz'

# Skip s3 backend from prune

docker compose up -d
sleep 5

info "Create backup with no prune for s3 backend"
docker compose exec backup backup

info "Check if old backup has been pruned (local)"
if [ -f "$LOCAL_DIR/test-hostnametoken-old.tar.gz" ]; then
  fail "Expired backup was not pruned from local storage."
fi

info "Check if old backup has NOT been pruned (s3)"
docker run --rm \
  -v minio_backup_data:/minio_data \
  alpine \
  ash -c 'test -f /minio_data/backup/test-hostnametoken-old.tar.gz'

pass "Old remote backup has been pruned locally, skipped S3 backend is untouched."

# Skip local and s3 backend from prune (all backends)

touch -r "$LOCAL_DIR/test-hostnametoken.tar.gz" -d "14 days ago" "$LOCAL_DIR/test-hostnametoken-old.tar.gz"

docker compose up -d
sleep 5

info "Create backup with no prune for both backends"
docker compose exec -e BACKUP_SKIP_BACKENDS_FROM_PRUNE="s3,local" backup backup

info "Check if old backup has NOT been pruned (local)"
if [ ! -f "$LOCAL_DIR/test-hostnametoken-old.tar.gz" ]; then
  fail "Backdated file has not been deleted"
fi

info "Check if old backup has NOT been pruned (s3)"
docker run --rm \
  -v minio_backup_data:/minio_data \
  alpine \
  ash -c 'test -f /minio_data/backup/test-hostnametoken-old.tar.gz'

pass "Skipped all backends while pruning."
