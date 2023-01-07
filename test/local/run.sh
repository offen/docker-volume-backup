#!/bin/sh

set -e

cd "$(dirname "$0")"
. ../util.sh
current_test=$(basename $(pwd))

mkdir -p local

docker compose up -d
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
# TODO: find out if we can test actual deletion without having to wait for a day
BACKUP_RETENTION_DAYS="0" docker compose up -d
sleep 5

docker compose exec backup backup

if [ "$(find ./local -type f | wc -l)" != "1" ]; then
  fail "Backups should not have been deleted, instead seen: "$(find ./local -type f)""
fi
pass "Local backups have not been deleted."

docker compose down --volumes
