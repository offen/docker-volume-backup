#!/bin/sh

# Copyright 2021 - Offen Authors <hioffen@posteo.de>
# SPDX-License-Identifier: MPL-2.0

# Portions of this file are taken from github.com/futurice/docker-volume-backup
# See NOTICE for information about authors and licensing.

source env.sh

function info {
  echo -e "\n[INFO] $1\n"
}

info "Preparing backup"
DOCKER_SOCK="/var/run/docker.sock"

if [ -S "$DOCKER_SOCK" ]; then
  TEMPFILE="$(mktemp)"
  docker ps -q \
    --filter "label=docker-volume-backup.stop-during-backup=$BACKUP_STOP_CONTAINER_LABEL" \
    > "$TEMPFILE"
  CONTAINERS_TO_STOP="$(cat $TEMPFILE | tr '\n' ' ')"
  CONTAINERS_TO_STOP_TOTAL="$(cat $TEMPFILE | wc -l)"
  CONTAINERS_TOTAL="$(docker ps -q | wc -l)"
  rm "$TEMPFILE"
  echo "$CONTAINERS_TOTAL containers running on host in total."
  echo "$CONTAINERS_TO_STOP_TOTAL containers marked to be stopped during backup."
else
  CONTAINERS_TO_STOP_TOTAL="0"
  CONTAINERS_TOTAL="0"
  echo "Cannot access \"$DOCKER_SOCK\", won't look for containers to stop."
fi

if [ "$CONTAINERS_TO_STOP_TOTAL" != "0" ]; then
  info "Stopping containers"
  docker stop $CONTAINERS_TO_STOP
fi

info "Creating backup"
BACKUP_FILENAME="$(date +"$BACKUP_FILENAME")"
tar -czvf "$BACKUP_FILENAME" $BACKUP_SOURCES # allow the var to expand, in case we have multiple sources

if [ ! -z "$GPG_PASSPHRASE" ]; then
  info "Encrypting backup"
  gpg --symmetric --cipher-algo aes256 --batch --passphrase "$GPG_PASSPHRASE" \
    -o "${BACKUP_FILENAME}.gpg" $BACKUP_FILENAME
  rm $BACKUP_FILENAME
  BACKUP_FILENAME="${BACKUP_FILENAME}.gpg"
fi

if [ "$CONTAINERS_TO_STOP_TOTAL" != "0" ]; then
  info "Starting containers/services back up"
  # The container might be part of a stack when running in swarm mode, so
  # its parent service needs to be restarted instead once backup is finished.
  SERVICES_REQUIRING_UPDATE=""
  for CONTAINER_ID in $CONTAINERS_TO_STOP; do
    SWARM_SERVICE_NAME=$(
      docker inspect \
        --format "{{ index .Config.Labels \"com.docker.swarm.service.name\" }}" \
        $CONTAINER_ID
    )
    if [ -z "$SWARM_SERVICE_NAME" ]; then
      echo "Restarting $(docker start $CONTAINER_ID)"
    else
      echo "Removing $(docker rm $CONTAINER_ID)"
      # Multiple containers might belong to the same service, so they will
      # be restarted only after all names are known.
      SERVICES_REQUIRING_UPDATE="${SERVICES_REQUIRING_UPDATE} ${SWARM_SERVICE_NAME}"
    fi
  done

  if [ -n "$SERVICES_REQUIRING_UPDATE" ]; then
    for SERVICE_NAME in $(echo -n "$SERVICES_REQUIRING_UPDATE" | tr ' ' '\n' | sort -u); do
      docker service update --force $SERVICE_NAME
    done
  fi
fi

copy_backup () {
  mc cp $MC_GLOBAL_OPTIONS "$BACKUP_FILENAME" "$1"
}

if [ ! -z "$AWS_S3_BUCKET_NAME" ]; then
  info "Uploading backup to remote storage"
  echo "Will upload to bucket \"$AWS_S3_BUCKET_NAME\"."
  copy_backup "backup-target/$AWS_S3_BUCKET_NAME"
  echo "Upload finished."
fi

if [ -d "$BACKUP_ARCHIVE" ]; then
  info "Copying backup to local archive"
  echo "Will copy to \"$BACKUP_ARCHIVE\"."
  copy_backup "$BACKUP_ARCHIVE"
  echo "Finished copying."
fi

if [ -f "$BACKUP_FILENAME" ]; then
  info "Cleaning up"
  rm -vf "$BACKUP_FILENAME"
fi

info "Backup finished"
echo "Will wait for next scheduled backup."

prune () {
  target=$1
  if [ ! -z "$BACKUP_PRUNING_PREFIX" ]; then
    target="$target/${BACKUP_PRUNING_PREFIX}"
  fi

  rule_applies_to=$(
    mc rm $MC_GLOBAL_OPTIONS --fake --recursive --force \
      --older-than "${BACKUP_RETENTION_DAYS}d" \
      "$target" \
      | wc -l
  )
  if [ "$rule_applies_to" == "0" ]; then
    echo "No backups found older than the configured retention period of $BACKUP_RETENTION_DAYS days."
    echo "Doing nothing."
    exit 0
  fi

  total=$(mc ls $MC_GLOBAL_OPTIONS "$target" | wc -l)

  if [ "$rule_applies_to" == "$total" ]; then
    echo "Using a retention of ${BACKUP_RETENTION_DAYS} days would prune all currently existing backups, will not continue."
    echo "If this is what you want, please remove files manually instead of using this script."
    exit 1
  fi

  mc rm $MC_GLOBAL_OPTIONS \
    --recursive --force \
    --older-than "${BACKUP_RETENTION_DAYS}d" "$target"
  echo "Successfully pruned ${rule_applies_to} backups older than ${BACKUP_RETENTION_DAYS} days."
}

if [ ! -z "$BACKUP_RETENTION_DAYS" ]; then
  info "Pruning old backups"
  echo "Sleeping ${BACKUP_PRUNING_LEEWAY} before checking eligibility."
  sleep "$BACKUP_PRUNING_LEEWAY"
  if [ ! -z "$AWS_S3_BUCKET_NAME" ]; then
    info "Pruning old backups from remote storage"
    prune "backup-target/$bucket"
  fi
  if [ -d "$BACKUP_ARCHIVE" ]; then
    info "Pruning old backups from local archive"
    prune "$BACKUP_ARCHIVE"
  fi
fi
