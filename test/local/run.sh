#!/bin/sh

set -e

cd "$(dirname "$0")"
. ../util.sh
current_test=$(basename $(pwd))

mkdir -p local

docker compose up -d --quiet-pull
sleep 5

# A symlink for a known file in the volume is created so the test can check
# whether symlinks are preserved on backup.
docker compose exec offen ln -s /var/opt/offen/offen.db /var/opt/offen/db.link
docker compose exec backup backup

sleep 5

expect_running_containers "2"

tmp_dir=$(mktemp -d)
tar -xvf ./local/test-hostnametoken.tar.gz -C $tmp_dir
if [ ! -f "$tmp_dir/backup/app_data/offen.db" ]; then
  fail "Could not find expected file in untared archive."
fi
rm -f ./local/test-hostnametoken.tar.gz

if [ ! -L "$tmp_dir/backup/app_data/db.link" ]; then
  fail "Could not find expected symlink in untared archive."
fi

pass "Found relevant files in decrypted and untared local backup."

if [ ! -L ./local/test-hostnametoken.latest.tar.gz.gpg ]; then
  fail "Could not find symlink to latest version."
fi

pass "Found symlink to latest version in local backup."

# The second part of this test checks if backups get deleted when the retention
# is set to 0 days (which it should not as it would mean all backups get deleted)
BACKUP_RETENTION_DAYS="0" docker compose up -d --timeout 3
sleep 5

docker compose exec backup backup

if [ "$(find ./local -type f | wc -l)" != "1" ]; then
  fail "Backups should not have been deleted, instead seen: "$(find ./local -type f)""
fi
pass "Local backups have not been deleted."

# The third part of this test checks if old backups get deleted when the retention
# is set to 7 days (which it should)

BACKUP_RETENTION_DAYS="7" docker compose up -d --timeout 3
sleep 5

info "Create first backup with no prune"
docker compose exec backup backup

touch -r ./local/test-hostnametoken.tar.gz -d "14 days ago" ./local/test-hostnametoken-old.tar.gz

info "Create second backup and prune"
docker compose exec backup backup

test ! -f ./local/test-hostnametoken-old.tar.gz
test -f ./local/test-hostnametoken.tar.gz

pass "Old remote backup has been pruned, new one is still present."

docker compose down --volumes --timeout 3
