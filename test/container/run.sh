#!/bin/sh

set -e

cd "$(dirname "$0")"
. ../util.sh
current_test=$(basename $(pwd))

export LOCAL_DIR=$(mktemp -d)

docker compose up -d --quiet-pull
sleep 5

# A symlink for a known file in the volume is created so the test can check
# whether symlinks are preserved on backup.
docker compose exec offen ln -s /var/opt/offen/offen.db /var/opt/offen/db.link
docker compose exec backup backup

sleep 5

expect_running_containers "2"

tmp_dir=$(mktemp -d)
tar -xvf "$LOCAL_DIR/test.tar.gz" -C $tmp_dir
if [ ! -f "$tmp_dir/backup/app_data/offen.db" ]; then
  fail "Could not find expected file in untared archive."
fi
rm -f "$LOCAL_DIR/test-hostnametoken.tar.gz"
