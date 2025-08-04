#!/bin/sh

set -e

cd "$(dirname "$0")"
. ../util.sh
current_test=$(basename $(pwd))

export SPEC_FILE=$(mktemp -d)/googledrive_v3.yaml
cp googledrive_v3.yaml $SPEC_FILE
sed -i 's/CREATED_TIME_1/'"$(date "+%Y-%m-%dT%H:%M:%SZ")/g" $SPEC_FILE
sed -i 's/CREATED_TIME_2/'"$(date "+%Y-%m-%dT%H:%M:%SZ" -d "14 days ago")/g" $SPEC_FILE

docker compose up -d --quiet-pull
sleep 5

logs=$(docker compose exec backup backup | tee /dev/stderr)

sleep 5

expect_running_containers "4"

if echo "$logs" | grep -q "ERROR"; then
  fail "Backup failed, check logs for error"
else
  pass "Backup succeeded, no errors reported."
fi

# The second part of this test checks if backups get deleted when the retention
# is set to 0 days (which it should not as it would mean all backups get deleted)
BACKUP_RETENTION_DAYS="0" docker compose up -d
sleep 5

logs=$(docker compose exec -T backup backup | tee /dev/stderr)

if echo "$logs" | grep -q "Refusing to do so, please check your configuration"; then
  pass "Remote backups have not been deleted."
else
  fail "Remote backups would have been deleted: $logs"
fi

# The third part of this test checks if old backups get deleted when the retention
# is set to 7 days (which it should)
BACKUP_RETENTION_DAYS="7" docker compose up -d
sleep 5

info "Create second backup and prune"

logs=$(docker compose exec -T backup backup | tee /dev/stderr)

if echo "$logs" | grep -q "Pruned 1 out of 2 backups as they were older"; then
  pass "Old remote backup has been pruned, new one is still present."
elif echo "$logs" | grep -q "ERROR"; then
  fail "Pruning failed, errors reported: $logs"
elif echo "$logs" | grep -q "None of 1 existing backups were pruned"; then
  fail "Pruning failed, old backup has not been pruned: $logs"
else
  fail "Pruning failed, unknown result: $logs"
fi
