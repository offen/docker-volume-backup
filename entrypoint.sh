#!/bin/sh

# Copyright 2021 - Offen Authors <hioffen@posteo.de>
# SPDX-License-Identifier: MPL-2.0

set -e

if [ ! -d "/etc/dockervolumebackup/conf.d" ]; then
  BACKUP_CRON_EXPRESSION="${BACKUP_CRON_EXPRESSION:-@daily}"

  echo "Installing cron.d entry with expression $BACKUP_CRON_EXPRESSION."
  echo "$BACKUP_CRON_EXPRESSION backup 2>&1" | crontab -
else
  echo "/etc/dockervolumebackup/conf.d was found, using configuration files from this directory."

  crontab -r && crontab /dev/null
  for file in /etc/dockervolumebackup/conf.d/*; do
    source $file
    BACKUP_CRON_EXPRESSION="${BACKUP_CRON_EXPRESSION:-@daily}"
    echo "Appending cron.d entry with expression $BACKUP_CRON_EXPRESSION and configuration file $file"
    (crontab -l; echo "$BACKUP_CRON_EXPRESSION /bin/sh -c 'set -a; source $file; set +a && backup' 2>&1") | crontab -
  done
fi

if [ ! -z "$SERVE_METRICS_PATH" ]; then
  mkdir -p /var/www/html${SERVE_METRICS_PATH}
  echo "ok" > /var/www/html${SERVE_METRICS_PATH}/metrics.txt
  httpd -h /var/www/html -p "${SERVE_METRICS_PORT:-80}"
  echo "Serving metrics on port ${SERVE_METRICS_PORT:-80}."
fi

echo "Starting cron in foreground."
crond -f -d 8
