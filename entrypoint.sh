#!/bin/sh

# Copyright 2021 - Offen Authors <hioffen@posteo.de>
# SPDX-License-Identifier: MPL-2.0

set -e

BACKUP_CRON_EXPRESSION="${BACKUP_CRON_EXPRESSION:-@daily}"

echo "Installing cron.d entry with expression $BACKUP_CRON_EXPRESSION."
echo "$BACKUP_CRON_EXPRESSION backup 2>&1" | crontab -

echo "Starting cron in foreground."
crond -f -l 8
