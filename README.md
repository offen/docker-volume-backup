# docker-volume-backup

Backup Docker volumes to any S3 compatible storage.

The [offen/docker-volume-backup](https://hub.docker.com/r/offen/docker-volume-backup) Docker image can be used as a sidecar container to an existing Docker setup. It handles recurring backups of Docker volumes to any S3 compatible storage and rotates away old backups if configured.

## Configuration

Backup targets, schedule and retention are configured in environment variables:

```ini
########### BACKUP SCHEDULE

# Backups run on the given cron schedule and use the filename defined in the
# template expression.

BACKUP_CRON_EXPRESSION="0 2 * * *"
BACKUP_FILENAME="offen-db-%Y-%m-%dT%H-%M-%S.tar.gz"

########### BACKUP STORAGE

# Define credentials for authenticating against the backup storage and a bucket
# name. Although all of these values are `AWS`-prefixed, the setup can be used
# with any S3 compatible storage.

AWS_ACCESS_KEY_ID="<xxx>"
AWS_SECRET_ACCESS_KEY="<xxx>"
AWS_S3_BUCKET_NAME="<xxx>"

# This is the FQDN of your storage server, e.g. `storage.example.com`.
# Do not set this when working against AWS S3.
# AWS_ENDPOINT="<xxx>"

# The protocol to be used when communicating with your storage server.
# Defaults to "https". You can set this to "http" when communicating with
# a different Docker container on the same host for example.
# AWS_ENDPOINT_PROTO="https"

########### BACKUP PRUNING

# Define this value to enable automatic pruning of old backups. The value
# declares the number of days for which a backup is kept.

# BACKUP_RETENTION_DAYS="7"

# In case the duration a backup takes fluctuates noticeably in your setup
# you can adjust this setting to make sure there are no race conditions
# between the backup finishing and the pruning not deleting backups that
# sit on the very edge of the time window. Set this value to a duration
# that is expected to be bigger than the maximum difference of backups.
# Valid values have a suffix of (s)econds, (m)inutes, (h)ours, or (d)ays.

# BACKUP_PRUNING_LEEWAY="10m"

########### BACKUP ENCRYPTION

# Backups can be encrypted using gpg in case a passphrase is given

# GPG_PASSPHRASE="<xxx>"

########### STOPPING CONTAINERS DURING BACKUP

# Containers can be stopped by applying a
# `docker-volume-backup.stop-during-backup` label. By default, all containers
# that are labeled with `true` will be stopped. If you need more fine grained
# control (e.g. when running multiple containers based on this image), you can
# override this default by specifying a different value here.

# BACKUP_STOP_CONTAINER_LABEL="service1"

########### MINIO CLIENT CONFIGURATION

# Pass these additional flags to all MinIO client `mc` invocations.
# This can be used for example to pass `--insecure` when using self
# signed certificates, or passing `--debug` to gain insights on
# unexpected behavior.

# MC_GLOBAL_OPTIONS="<xxx>"
```

## Example in a docker-compose setup

Most likely, you will use this image as a sidecar container in an existing docker-compose setup like this:

```yml
version: '3'

services:
  volume-consumer:
    build:
      context: ./my-app
    volumes:
      - data:/var/my-app
    labels:
      # This means the container will be stopped during backup to ensure
      # backup integrity. You can omit this label if stopping during backup
      # not required.
      - docker-volume-backup.stop-during-backup=true

  backup:
    image: offen/docker-volume-backup:latest
    restart: always
    env_file: ./backup.env
    volumes:
      # Mounting the Docker socket allows the script to stop and restart
      # the container during backup. You can omit this if you don't want
      # to stop the container
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - data:/backup/my-app-backup:ro
volumes:
  data:
```

## Using with Docker Swarm

By default, Docker Swarm will restart stopped containers automatically, even when manually stopped. If you plan to have your containers / services stopped during backup, this means you need to apply the `on-failure` restart policy to your service's definitions. A restart policy of `always` is not compatible with this tool.

---

When running in Swarm mode, it's also advised to set a hard memory limit on your service (~25MB should be enough in most cases, but if you backup large files above half a gigabyte or similar, you might have to raise this in case the backup exits with `Killed`):

```yml
services:
  backup:
    image: offen/docker-volume-backup:latest
    deployment:
      resources:
        limits:
          memory: 25M
```

## Manually triggering a backup

You can manually trigger a backup run outside of the defined cron schedule by executing the `backup` command inside the container:

```
docker exec <container_ref> backup
```

---

## Differences to `futurice/docker-volume-backup`

This image is heavily inspired by the `futurice/docker-volume-backup`. We decided to publish this image as a simpler and more lightweight alternative because of the following requirements:

- The original image is based on `ubuntu`, making it very heavy. This version is roughly 1/3 in compressed size.
- This image makes use of the MinIO client `mc` instead of the full blown AWS CLI for uploading backups.
- The original image proposed to handle backup rotation through AWS S3 lifecycle policies. This image adds the option to rotate old backups through the same script so this functionality can also be offered for non-AWS storage backends like MinIO.
- InfluxDB specific functionality was removed.
- `arm64` and `arm/v7` architectures are supported.
- Docker in Swarm mode is supported.
