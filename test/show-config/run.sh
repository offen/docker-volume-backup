#!/bin/sh

set -e

cd "$(dirname "$0")"
. ../util.sh
current_test=$(basename $(pwd))

info "show-config with environment variables"
docker compose up -d --quiet-pull
logs=$(docker compose exec -T backup backup show-config)

echo "$logs"

if ! echo "$logs" | grep -q "source=from environment"; then
  fail "Missing source line."
fi
pass "Source line present."

if ! echo "$logs" | grep -q "BackupSources:/backup"; then
  fail "Missing BACKUP_SOURCES in output."
fi
pass "BACKUP_SOURCES present."

if ! echo "$logs" | grep -q "BackupFilename:backup-expanded.tar"; then
  fail "Missing expanded BACKUP_FILENAME in output."
fi
pass "Expanded BACKUP_FILENAME present."

if ! echo "$logs" | grep -q "NotificationURLs:\[stdout://\]"; then
  fail "Missing NOTIFICATION_URLS in output."
fi
pass "NOTIFICATION_URLS present."

if ! echo "$logs" | grep -q "AwsS3BucketName:example-bucket"; then
  fail "Missing AWS_S3_BUCKET_NAME in output."
fi
pass "AWS_S3_BUCKET_NAME present."

docker compose down

info "show-config with conf.d and _FILE"
export CONF_DIR=$(pwd)/conf.d
export SECRET_FILE=$(mktemp)
printf "stdout://\n" > "$SECRET_FILE"

docker compose -f docker-compose.confd.yml up -d --quiet-pull
logs=$(docker compose -f docker-compose.confd.yml exec -T backup backup show-config)

echo "$logs"

if ! echo "$logs" | grep -q "source=01show-config.env"; then
  fail "Missing conf.d source line."
fi
pass "conf.d source line present."

if ! echo "$logs" | grep -q "BackupSources:/conf-backup"; then
  fail "Missing conf.d BACKUP_SOURCES in output."
fi
pass "conf.d BACKUP_SOURCES present."

if ! echo "$logs" | grep -q "NotificationURLs:\\[stdout://"; then
  fail "Missing conf.d NOTIFICATION_URLS in output."
fi
pass "conf.d NOTIFICATION_URLS present."

docker compose -f docker-compose.confd.yml down
rm -f "$SECRET_FILE"
