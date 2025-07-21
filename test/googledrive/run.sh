#!/bin/sh

set -e

cd "$(dirname "$0")"
. ../util.sh
current_test=$(basename $(pwd))

export SPEC_FILE=$(mktemp -d)/googledrive_v3.yaml
cp googledrive_v3.yaml $SPEC_FILE
sed -i 's/SERVER_MODIFIED_1/'"$(date "+%Y-%m-%dT%H:%M:%SZ")/g" $SPEC_FILE
sed -i 's/SERVER_MODIFIED_2/'"$(date "+%Y-%m-%dT%H:%M:%SZ" -d "14 days ago")/g" $SPEC_FILE

docker compose up -d --quiet-pull
sleep 5
set +e
logs=$(docker compose exec backup backup 2>&1)
set -e
sleep 5

expect_running_containers "4"

echo "$logs"
if echo "$logs" | grep -q "ERROR"; then
  fail "Backup failed, check logs for error"
else
  pass "Backup succeeded, no errors reported."
fi

# The second part of this test checks if backups get deleted when the retention
# is set to 0 days (which it should not as it would mean all backups get deleted)
BACKUP_RETENTION_DAYS="0" docker compose up -d
sleep 5

set +e
logs=$(docker compose exec -T backup backup 2>&1)
set -e

echo "$logs"
if echo "$logs" | grep -q "ERROR"; then
  fail "Retention protection for 0 days failed, check logs for error"
else
  pass "Retention protection for 0 days succeeded, no errors reported."
fi
# The third part of this test checks if old backups get deleted when the retention
# is set to 7 days (which it should)
BACKUP_RETENTION_DAYS="7" docker compose up -d
sleep 5

info "Create second backup and prune"
set +e
logs=$(docker compose exec -T backup backup 2>&1)
set -e

echo "$logs"
if echo "$logs" | grep -q "ERROR"; then
  fail "Prunning failed, check logs for error"
else
  pass "Prunning succeeded, no errors reported."
fi
