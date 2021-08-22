#!/bin/sh

# Copyright 2021 - Offen Authors <hioffen@posteo.de>
# SPDX-License-Identifier: MPL-2.0

# Portions of this file are taken from github.com/futurice/docker-volume-backup
# See NOTICE for information about authors and licensing.

set -e

# Write cronjob env to file, fill in sensible defaults, and read them back in
mkdir -p /etc/backup
cat <<EOF > /etc/backup.env
BACKUP_SOURCES="${BACKUP_SOURCES:-/backup}"
BACKUP_CRON_EXPRESSION="${BACKUP_CRON_EXPRESSION:-@daily}"
BACKUP_FILENAME="${BACKUP_FILENAME:-backup-%Y-%m-%dT%H-%M-%S.tar.gz}"
BACKUP_ARCHIVE="${BACKUP_ARCHIVE:-/archive}"

BACKUP_RETENTION_DAYS="${BACKUP_RETENTION_DAYS:-}"
BACKUP_PRUNING_LEEWAY="${BACKUP_PRUNING_LEEWAY:-10m}"
BACKUP_PRUNING_PREFIX="${BACKUP_PRUNING_PREFIX:-}"
BACKUP_STOP_CONTAINER_LABEL="${BACKUP_STOP_CONTAINER_LABEL:-true}"

AWS_S3_BUCKET_NAME="${AWS_S3_BUCKET_NAME:-}"
AWS_ENDPOINT="${AWS_ENDPOINT:-s3.amazonaws.com}"
AWS_ENDPOINT_PROTO="${AWS_ENDPOINT_PROTO:-https}"

GPG_PASSPHRASE="${GPG_PASSPHRASE:-}"

MC_GLOBAL_OPTIONS="${MC_GLOBAL_OPTIONS:-}"
EOF
chmod a+x /etc/backup.env
source /etc/backup.env

# Add our cron entry, and direct stdout & stderr to Docker commands stdout
echo "Installing cron.d entry with expression $BACKUP_CRON_EXPRESSION."
echo "$BACKUP_CRON_EXPRESSION backup 2>&1" | crontab -

# Let cron take the wheel
echo "Starting cron in foreground."
crond -f -l 8
