#!/bin/sh

# Copyright 2021 - Offen Authors <hioffen@posteo.de>
# SPDX-License-Identifier: MPL-2.0

# Portions of this file are taken from github.com/futurice/docker-volume-backup
# See NOTICE for information about authors and licensing.

set -e

# Write cronjob env to file, fill in sensible defaults, and read them back in
cat <<EOF > env.sh
BACKUP_SOURCES="${BACKUP_SOURCES:-/backup}"
BACKUP_CRON_EXPRESSION="${BACKUP_CRON_EXPRESSION:-@daily}"
BACKUP_FILENAME=${BACKUP_FILENAME:-"backup-%Y-%m-%dT%H-%M-%S.tar.gz"}

BACKUP_RETENTION_DAYS="${BACKUP_RETENTION_DAYS:-}"

AWS_S3_BUCKET_NAME="${AWS_S3_BUCKET_NAME:-}"
AWS_ENDPOINT="${AWS_ENDPOINT:-s3.amazonaws.com}"

GPG_PASSPHRASE="${GPG_PASSPHRASE:-}"
EOF
chmod a+x env.sh
source env.sh

mc alias set backup-target "https://$AWS_ENDPOINT" "$AWS_ACCESS_KEY_ID" "$AWS_SECRET_ACCESS_KEY"

# Add our cron entry, and direct stdout & stderr to Docker commands stdout
echo "Installing cron.d entry with expression $BACKUP_CRON_EXPRESSION."
echo "$BACKUP_CRON_EXPRESSION backup 2>&1" | crontab -

# Let cron take the wheel
echo "Starting cron in foreground."
crond -f -l 8
