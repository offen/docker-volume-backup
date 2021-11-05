# docker-volume-backup

Backup Docker volumes locally or to any S3 compatible storage.

The [offen/docker-volume-backup](https://hub.docker.com/r/offen/docker-volume-backup) Docker image can be used as a lightweight (below 15MB) sidecar container to an existing Docker setup.
It handles __recurring or one-off backups of Docker volumes__ to a __local directory__ or __any S3 compatible storage__ (or both), and __rotates away old backups__ if configured. It also supports __encrypting your backups using GPG__ and __sending notifications for failed backup runs__.

<!-- MarkdownTOC -->

- [Quickstart](#quickstart)
  - [Recurring backups in a compose setup](#recurring-backups-in-a-compose-setup)
  - [One-off backups using Docker CLI](#one-off-backups-using-docker-cli)
- [Configuration reference](#configuration-reference)
- [How to](#how-to)
  - [Stopping containers during backup](#stopping-containers-during-backup)
  - [Automatically pruning old backups](#automatically-pruning-old-backups)
  - [Send email notifications on failed backup runs](#send-email-notifications-on-failed-backup-runs)
  - [Encrypting your backup using GPG](#encrypting-your-backup-using-gpg)
  - [Restoring a volume from a backup](#restoring-a-volume-from-a-backup)
  - [Set the timezone the container runs in](#set-the-timezone-the-container-runs-in)
  - [Using with Docker Swarm](#using-with-docker-swarm)
  - [Manually triggering a backup](#manually-triggering-a-backup)
- [Recipes](#recipes)
  - [Backing up to AWS S3](#backing-up-to-aws-s3)
  - [Backing up to MinIO](#backing-up-to-minio)
  - [Backing up locally](#backing-up-locally)
  - [Backing up to AWS S3 as well as locally](#backing-up-to-aws-s3-as-well-as-locally)
  - [Running on a custom cron schedule](#running-on-a-custom-cron-schedule)
  - [Rotating away backups that are older than 7 days](#rotating-away-backups-that-are-older-than-7-days)
  - [Encrypting your backups using GPG](#encrypting-your-backups-using-gpg)
  - [Running multiple instances in the same setup](#running-multiple-instances-in-the-same-setup)
- [Differences to `futurice/docker-volume-backup`](#differences-to-futuricedocker-volume-backup)

<!-- /MarkdownTOC -->

---

Code and documentation for `v1` versions are found on [this branch][v1-branch].

[v1-branch]: https://github.com/offen/docker-volume-backup/tree/v1

## Quickstart

### Recurring backups in a compose setup

Add a `backup` service to your compose setup and mount the volumes you would like to see backed up:

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
    # In production, it is advised to lock your image tag to a proper
    # release version instead of using `latest`.
    # Check https://github.com/offen/docker-volume-backup/releases
    # for a list of available releases.
    image: offen/docker-volume-backup:latest
    restart: always
    env_file: ./backup.env # see below for configuration reference
    volumes:
      - data:/backup/my-app-backup:ro
      # Mounting the Docker socket allows the script to stop and restart
      # the container during backup. You can omit this if you don't want
      # to stop the container
      - /var/run/docker.sock:/var/run/docker.sock:ro
      # If you mount a local directory or volume to `/archive` a local
      # copy of the backup will be stored there. You can override the
      # location inside of the container by setting `BACKUP_ARCHIVE`.
      # You can omit this if you do not want to keep local backups.
      - /path/to/local_backups:/archive
volumes:
  data:
```

### One-off backups using Docker CLI

To run a one time backup, mount the volume you would like to see backed up into a container and run the `backup` command:

```console
docker run --rm \
  -v data:/backup/data \
  --env AWS_ACCESS_KEY_ID="<xxx>" \
  --env AWS_SECRET_ACCESS_KEY="<xxx>" \
  --env AWS_S3_BUCKET_NAME="<xxx>" \
  --entrypoint backup \
  offen/docker-volume-backup:latest
```

Alternatively, pass a `--env-file` in order to use a full config as described below.

## Configuration reference

Backup targets, schedule and retention are configured in environment variables.
You can populate below template according to your requirements and use it as your `env_file`:

```ini
########### BACKUP SCHEDULE

# Backups run on the given cron schedule in `busybox` flavor. If no
# value is set, `@daily` will be used. If you do not want the cron
# to ever run, use `0 0 5 31 2 ?`.

# BACKUP_CRON_EXPRESSION="0 2 * * *"

# The name of the backup file including the `.tar.gz` extension.
# Format verbs will be replaced as in `strftime`. Omitting them
# will result in the same filename for every backup run, which means previous
# versions will be overwritten on subsequent runs. The default results
# in filenames like `backup-2021-08-29T04-00-00.tar.gz`.

# BACKUP_FILENAME="backup-%Y-%m-%dT%H-%M-%S.tar.gz"

# When storing local backups, a symlink to the latest backup can be created
# in case a value is given for this key. This has no effect on remote backups.

# BACKUP_LATEST_SYMLINK="backup.latest.tar.gz"

# Wehter to copy the content of backup folder before creating archive.
# In rare scenario where the content of the source backup volume is still
# updating, but we do not wish to stop the container while performing backup,
# this snapshot before archive ensures the integrity of the tar.gz file.

# BACKUP_FROM_SNAPSHOT="false"

########### BACKUP STORAGE

# The name of the remote bucket that should be used for storing backups. If
# this is not set, no remote backups will be stored.

# AWS_S3_BUCKET_NAME="backup-bucket"

# Define credentials for authenticating against the backup storage and a bucket
# name. Although all of these keys are `AWS`-prefixed, the setup can be used
# with any S3 compatible storage.

# AWS_ACCESS_KEY_ID="<xxx>"
# AWS_SECRET_ACCESS_KEY="<xxx>"

# Instead of providing static credentials, you can also use IAM instance profiles
# or similar to provide authentication. Some possible configuration options on AWS:
# - EC2: http://169.254.169.254
# - ECS: http://169.254.170.2

# AWS_IAM_ROLE_ENDPOINT="http://169.254.169.254"

# This is the FQDN of your storage server, e.g. `storage.example.com`.
# Do not set this when working against AWS S3 (the default value is 
# `s3.amazonaws.com`). If you need to set a specific (non-https) protocol, you
# will need to use the option below.

# AWS_ENDPOINT="storage.example.com"

# The protocol to be used when communicating with your storage server.
# Defaults to "https". You can set this to "http" when communicating with
# a different Docker container on the same host for example.

# AWS_ENDPOINT_PROTO="https"

# Setting this variable to `true` will disable verification of
# SSL certificates. You shouldn't use this unless you use self-signed
# certificates for your remote storage backend. This can only be used
# when AWS_ENDPOINT_PROTO is set to `https`.

# AWS_ENDPOINT_INSECURE="true"

# In addition to storing backups remotely, you can also keep local copies.
# Pass a container-local path to store your backups if needed. You also need to
# mount a local folder or Docker volume into that location (`/archive`
# by default) when running the container. In case the specified directory does
# not exist (nothing is mounted) in the container when the backup is running,
# local backups will be skipped. Local paths are also be subject to pruning of
# old backups as defined below.

# BACKUP_ARCHIVE="/archive"

########### BACKUP PRUNING

# **IMPORTANT, PLEASE READ THIS BEFORE USING THIS FEATURE**:
# The mechanism used for pruning old backups is not very sophisticated
# and applies its rules to **all files in the target directory** by default,
# which means that if you are storing your backups next to other files,
# these might become subject to deletion too. When using this option
# make sure the backup files are stored in a directory used exclusively
# for such files, or to configure BACKUP_PRUNING_PREFIX to limit
# removal to certain files.

# Define this value to enable automatic rotation of old backups. The value
# declares the number of days for which a backup is kept.

# BACKUP_RETENTION_DAYS="7"

# In case the duration a backup takes fluctuates noticeably in your setup
# you can adjust this setting to make sure there are no race conditions
# between the backup finishing and the rotation not deleting backups that
# sit on the edge of the time window. Set this value to a duration
# that is expected to be bigger than the maximum difference of backups.
# Valid values have a suffix of (s)econds, (m)inutes or (h)ours. By default,
# one minute is used.

# BACKUP_PRUNING_LEEWAY="1m"

# In case your target bucket or directory contains other files than the ones
# managed by this container, you can limit the scope of rotation by setting
# a prefix value. This would usually be the non-parametrized part of your
# BACKUP_FILENAME. E.g. if BACKUP_FILENAME is `db-backup-%Y-%m-%dT%H-%M-%S.tar.gz`,
# you can set BACKUP_PRUNING_PREFIX to `db-backup-` and make sure
# unrelated files are not affected by the rotation mechanism.

# BACKUP_PRUNING_PREFIX="backup-"

########### BACKUP ENCRYPTION

# Backups can be encrypted using gpg in case a passphrase is given.

# GPG_PASSPHRASE="<xxx>"

########### STOPPING CONTAINERS DURING BACKUP

# Containers can be stopped by applying a
# `docker-volume-backup.stop-during-backup` label. By default, all containers
# that are labeled with `true` will be stopped. If you need more fine grained
# control (e.g. when running multiple containers based on this image), you can
# override this default by specifying a different value here.

# BACKUP_STOP_CONTAINER_LABEL="service1"

########### EMAIL NOTIFICATIONS ON FAILED BACKUP RUNS

# In case SMTP credentials are provided, notification emails can be sent out on
# failed backup runs. These emails will contain the start time, the error
# message and all log output prior to the failure.

# The recipient(s) of the notification. Supply a comma separated list
# of adresses if you want to notify multiple recipients. If this is
# not set, no emails will be sent.

# EMAIL_NOTIFICATION_RECIPIENT="you@example.com"

# The "From" header of the sent email. Defaults to `noreply@nohost`.

# EMAIL_NOTIFICATION_SENDER="no-reply@example.com"

# Configuration and credentials for the SMTP server to be used.
# EMAIL_SMTP_PORT defaults to 587.

# EMAIL_SMTP_HOST="posteo.de"
# EMAIL_SMTP_PASSWORD="<xxx>"
# EMAIL_SMTP_USERNAME="no-reply@example.com"
# EMAIL_SMTP_PORT="<port>"
```

## How to

### Stopping containers during backup

In many cases, it will be desirable to stop the services that are consuming the volume you want to backup in order to ensure data integrity.
This image can automatically stop and restart containers and services (in case you are running Docker in Swarm mode).
By default, any container that is labeled `docker-volume-backup.stop-during-backup=true` will be stopped before the backup is being taken and restarted once it has finished.

In case you need more fine grained control about which containers should be stopped (e.g. when backing up multiple volumes on different schedules), you can set the `BACKUP_STOP_CONTAINER_LABEL` environment variable and then use the same value for labeling:

```yml
version: '3'

services:
  app:
    # definition for app ...
    labels:
      - docker-volume-backup.stop-during-backup=service1

  backup:
    image: offen/docker-volume-backup:latest
    environment:
      BACKUP_STOP_CONTAINER_LABEL: service1
    volumes:
      - data:/backup/my-app-backup:ro
      - /var/run/docker.sock:/var/run/docker.sock:ro

volumes:
  data:
```

### Automatically pruning old backups

When `BACKUP_RETENTION_DAYS` is configured, the image will check if there are any backups in the remote bucket or local archive that are older than the given retention value and rotate these backups away.

Be aware that this mechanism looks at __all files in the target bucket or archive__, which means that other files that are older than the given deadline are deleted as well. In case you need to use a target that cannot be used exclusively for your backups, you can configure `BACKUP_PRUNING_PREFIX` to limit which files are considered eligible for deletion:

```yml
version: '3'

services:
  # ... define other services using the `data` volume here
  backup:
    image: offen/docker-volume-backup:latest
    environment:
      BACKUP_FILENAME: backup-%Y-%m-%dT%H-%M-%S.tar.gz
      BACKUP_PRUNING_PREFIX: backup-
      BACKUP_RETENTION_DAYS: 7
    volumes:
      - ${HOME}/backups:/archive
      - data:/backup/my-app-backup:ro
      - /var/run/docker.sock:/var/run/docker.sock:ro

volumes:
  data:
```

### Send email notifications on failed backup runs

To send out email notifications on failed backup runs, provide SMTP credentials, a sender and a recipient:

```yml
version: '3'

services:
  backup:
    image: offen/docker-volume-backup:latest
    environment:
      # ... other configuration values go here
      EMAIL_SMTP_HOST: "smtp.example.com"
      EMAIL_SMTP_PASSWORD: "password"
      EMAIL_SMTP_USERNAME: "username"
      EMAIL_NOTIFICATION_SENDER: "noreply@example.com"
      EMAIL_NOTIFICATION_RECIPIENT: "notifications@example.com"
```

### Encrypting your backup using GPG

The image supports encrypting backups using GPG out of the box.
In case a `GPG_PASSPHRASE` environment variable is set, the backup will be encrypted using the given key and saved as a `.gpg` file instead.

Assuming you have `gpg` installed, you can decrypt such a backup using (your OS will prompt for the passphrase before decryption can happen):

```console
gpg -o backup.tar.gz -d backup.tar.gz.gpg
```

### Restoring a volume from a backup

In case you need to restore a volume from a backup, the most straight forward procedure to do so would be:

- Stop the container(s) that are using the volume
- Untar the backup you want to restore
  ```console
  tar -C /tmp -xvf  backup.tar.gz
  ```
- Using a temporary one-off container, mount the volume (the example assumes it's named `data`) and copy over the backup. Make sure you copy the correct path level (this depends on how you mount your volume into the backup container), you might need to strip some leading elements
  ```console
  docker run -d --name backup_restore -v data:/backup_restore alpine
  docker cp /tmp/backup/data-backup backup_restore:/backup_restore
  docker stop backup_restore
  docker rm backup_restore
  ```
- Restart the container(s) that are using the volume 

Depending on your setup and the application(s) you are running, this might involve other steps to be taken still.

### Set the timezone the container runs in

By default a container based on this image will run in the UTC timezone.
As the image is designed to be as small as possible, additional timezone data is not included.
In case you want to run your cron rules in your local timezone (respecting DST and similar), you can mount your Docker host's `/etc/timezone` and `/etc/localtime` in read-only mode:

```yml
version: '3'

services:
  backup:
    image: offen/docker-volume-backup:latest
    volumes:
      - data:/backup/my-app-backup:ro
      - /etc/timezone:/etc/timezone:ro
      - /etc/localtime:/etc/localtime:ro

volumes:
  data:
```

### Using with Docker Swarm

By default, Docker Swarm will restart stopped containers automatically, even when manually stopped.
If you plan to have your containers / services stopped during backup, this means you need to apply the `on-failure` restart policy to your service's definitions.
A restart policy of `always` is not compatible with this tool.

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

### Manually triggering a backup

You can manually trigger a backup run outside of the defined cron schedule by executing the `backup` command inside the container:

```
docker exec <container_ref> backup
```

## Recipes

This section lists configuration for some real-world use cases that you can mix and match according to your needs.

### Backing up to AWS S3

```yml
version: '3'

services:
  # ... define other services using the `data` volume here
  backup:
    image: offen/docker-volume-backup:latest
    environment:
      AWS_BUCKET_NAME: backup-bucket
      AWS_ACCESS_KEY_ID: AKIAIOSFODNN7EXAMPLE
      AWS_SECRET_ACCESS_KEY: wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY
    volumes:
      - data:/backup/my-app-backup:ro
      - /var/run/docker.sock:/var/run/docker.sock:ro

volumes:
  data:
```

### Backing up to MinIO

```yml
version: '3'

services:
  # ... define other services using the `data` volume here
  backup:
    image: offen/docker-volume-backup:latest
    environment:
      AWS_ENDPOINT: minio.example.com
      AWS_BUCKET_NAME: backup-bucket
      AWS_ACCESS_KEY_ID: MINIOACCESSKEY
      AWS_SECRET_ACCESS_KEY: MINIOSECRETKEY
    volumes:
      - data:/backup/my-app-backup:ro
      - /var/run/docker.sock:/var/run/docker.sock:ro

volumes:
  data:
```

### Backing up locally

```yml
version: '3'

services:
  # ... define other services using the `data` volume here
  backup:
    image: offen/docker-volume-backup:latest
    environment:
      BACKUP_FILENAME: backup-%Y-%m-%dT%H-%M-%S.tar.gz
      BACKUP_LATEST_SYMLINK: backup-latest.tar.gz
    volumes:
      - data:/backup/my-app-backup:ro
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - ${HOME}/backups:/archive

volumes:
  data:
```

### Backing up to AWS S3 as well as locally

```yml
version: '3'

services:
  # ... define other services using the `data` volume here
  backup:
    image: offen/docker-volume-backup:latest
    environment:
      AWS_BUCKET_NAME: backup-bucket
      AWS_ACCESS_KEY_ID: AKIAIOSFODNN7EXAMPLE
      AWS_SECRET_ACCESS_KEY: wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY
    volumes:
      - data:/backup/my-app-backup:ro
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - ${HOME}/backups:/archive

volumes:
  data:
```

### Running on a custom cron schedule

```yml
version: '3'

services:
  # ... define other services using the `data` volume here
  backup:
    image: offen/docker-volume-backup:latest
    environment:
      # take a backup on every hour
      BACKUP_CRON_EXPRESSION: "0 * * * *"
      AWS_BUCKET_NAME: backup-bucket
      AWS_ACCESS_KEY_ID: AKIAIOSFODNN7EXAMPLE
      AWS_SECRET_ACCESS_KEY: wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY
    volumes:
      - data:/backup/my-app-backup:ro
      - /var/run/docker.sock:/var/run/docker.sock:ro

volumes:
  data:
```

### Rotating away backups that are older than 7 days

```yml
version: '3'

services:
  # ... define other services using the `data` volume here
  backup:
    image: offen/docker-volume-backup:latest
    environment:
      AWS_BUCKET_NAME: backup-bucket
      AWS_ACCESS_KEY_ID: AKIAIOSFODNN7EXAMPLE
      AWS_SECRET_ACCESS_KEY: wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY
      BACKUP_FILENAME: backup-%Y-%m-%dT%H-%M-%S.tar.gz
      BACKUP_PRUNING_PREFIX: backup-
      BACKUP_RETENTION_DAYS: 7
    volumes:
      - data:/backup/my-app-backup:ro
      - /var/run/docker.sock:/var/run/docker.sock:ro

volumes:
  data:
```

### Encrypting your backups using GPG

```yml
version: '3'

services:
  # ... define other services using the `data` volume here
  backup:
    image: offen/docker-volume-backup:latest
    environment:
      AWS_BUCKET_NAME: backup-bucket
      AWS_ACCESS_KEY_ID: AKIAIOSFODNN7EXAMPLE
      AWS_SECRET_ACCESS_KEY: wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY
      GPG_PASSPHRASE: somesecretstring
    volumes:
      - data:/backup/my-app-backup:ro
      - /var/run/docker.sock:/var/run/docker.sock:ro

volumes:
  data:
```

### Running multiple instances in the same setup

```yml
version: '3'

services:
  # ... define other services using the `data_1` and `data_2` volumes here
  backup_1: &backup_service
    image: offen/docker-volume-backup:latest
    environment: &backup_environment
      BACKUP_CRON_EXPRESSION: "0 2 * * *"
      AWS_BUCKET_NAME: backup-bucket
      AWS_ACCESS_KEY_ID: AKIAIOSFODNN7EXAMPLE
      AWS_SECRET_ACCESS_KEY: wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY
      # Label the container using the `data_1` volume as `docker-volume-backup.stop-during-backup=service1`
      BACKUP_STOP_CONTAINER_LABEL: service1
    volumes:
      - data_1:/backup/data-1-backup:ro
      - /var/run/docker.sock:/var/run/docker.sock:ro
  backup_2:
    <<: *backup_service
    environment:
      <<: *backup_environment
      # Label the container using the `data_2` volume as `docker-volume-backup.stop-during-backup=service2`
      BACKUP_CRON_EXPRESSION: "0 3 * * *"
      BACKUP_STOP_CONTAINER_LABEL: service2
    volumes:
      - data_2:/backup/data-2-backup:ro
      - /var/run/docker.sock:/var/run/docker.sock:ro

volumes:
  data_1:
  data_2:
```

## Differences to `futurice/docker-volume-backup`

This image is heavily inspired by `futurice/docker-volume-backup`. We decided to publish this image as a simpler and more lightweight alternative because of the following requirements:

- The original image is based on `ubuntu` and requires additional tools, making it heavy.
This version is roughly 1/25 in compressed size (it's ~12MB).
- The original image uses a shell script, when this version is written in Go, which makes it easier to extend and maintain (more verbose too).
- The original image proposed to handle backup rotation through AWS S3 lifecycle policies.
This image adds the option to rotate away old backups through the same command so this functionality can also be offered for non-AWS storage backends like MinIO.
Local copies of backups can also be pruned once they reach a certain age.
- InfluxDB specific functionality from the original image was removed.
- `arm64` and `arm/v7` architectures are supported.
- Docker in Swarm mode is supported.
- Notifications on failed backups are supported
- IAM authentication through instance profiles is supported
