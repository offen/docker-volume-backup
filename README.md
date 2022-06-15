<a href="https://www.offen.dev/">
    <img src="https://offen.github.io/press-kit/offen-material/gfx-GitHub-Offen-logo.svg" alt="Offen logo" title="Offen" width="150px"/>
</a>

# docker-volume-backup

Backup Docker volumes locally or to any S3 compatible storage.

The [offen/docker-volume-backup](https://hub.docker.com/r/offen/docker-volume-backup) Docker image can be used as a lightweight (below 15MB) sidecar container to an existing Docker setup.
It handles __recurring or one-off backups of Docker volumes__ to a __local directory__, __any S3, WebDAV or SSH compatible storage (or any combination) and rotates away old backups__ if configured. It also supports __encrypting your backups using GPG__ and __sending notifications for failed backup runs__.

<!-- MarkdownTOC -->

- [Quickstart](#quickstart)
  - [Recurring backups in a compose setup](#recurring-backups-in-a-compose-setup)
  - [One-off backups using Docker CLI](#one-off-backups-using-docker-cli)
- [Configuration reference](#configuration-reference)
- [How to](#how-to)
  - [Stop containers during backup](#stop-containers-during-backup)
  - [Automatically pruning old backups](#automatically-pruning-old-backups)
  - [Send email notifications on failed backup runs](#send-email-notifications-on-failed-backup-runs)
  - [Customize notifications](#customize-notifications)
  - [Run custom commands before / after backup](#run-custom-commands-before--after-backup)
  - [Encrypting your backup using GPG](#encrypting-your-backup-using-gpg)
  - [Restoring a volume from a backup](#restoring-a-volume-from-a-backup)
  - [Set the timezone the container runs in](#set-the-timezone-the-container-runs-in)
  - [Using with Docker Swarm](#using-with-docker-swarm)
  - [Manually triggering a backup](#manually-triggering-a-backup)
  - [Update deprecated email configuration](#update-deprecated-email-configuration)
  - [Replace deprecated `BACKUP_FROM_SNAPSHOT` usage](#replace-deprecated-backup_from_snapshot-usage)
  - [Using a custom Docker host](#using-a-custom-docker-host)
  - [Run multiple backup schedules in the same container](#run-multiple-backup-schedules-in-the-same-container)
  - [Define different retention schedules](#define-different-retention-schedules)
- [Recipes](#recipes)
  - [Backing up to AWS S3](#backing-up-to-aws-s3)
  - [Backing up to Filebase](#backing-up-to-filebase)
  - [Backing up to MinIO](#backing-up-to-minio)
  - [Backing up to WebDAV](#backing-up-to-webdav)
  - [Backing up to SSH](#backing-up-to-ssh)
  - [Backing up locally](#backing-up-locally)
  - [Backing up to AWS S3 as well as locally](#backing-up-to-aws-s3-as-well-as-locally)
  - [Running on a custom cron schedule](#running-on-a-custom-cron-schedule)
  - [Rotating away backups that are older than 7 days](#rotating-away-backups-that-are-older-than-7-days)
  - [Encrypting your backups using GPG](#encrypting-your-backups-using-gpg)
  - [Using mysqldump to prepare the backup](#using-mysqldump-to-prepare-the-backup)
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
      # to stop the container. In case you need to proxy the socket, you can
      # also provide a location by setting `DOCKER_HOST` in the container
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
  offen/docker-volume-backup:v2
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

# Setting BACKUP_FILENAME_EXPAND to true allows for environment variable
# placeholders in BACKUP_FILENAME, BACKUP_LATEST_SYMLINK and in
# BACKUP_PRUNING_PREFIX that will get expanded at runtime,
# e.g. `backup-$HOSTNAME-%Y-%m-%dT%H-%M-%S.tar.gz`. Expansion happens before
# interpolating strftime tokens. It is disabled by default.
# Please note that you will need to escape the `$` when providing the value
# in a docker-compose.yml file, i.e. using $$VAR instead of $VAR.

# BACKUP_FILENAME_EXPAND="true"

# When storing local backups, a symlink to the latest backup can be created
# in case a value is given for this key. This has no effect on remote backups.

# BACKUP_LATEST_SYMLINK="backup.latest.tar.gz"

# ************************************************************************
# The BACKUP_FROM_SNAPSHOT option has been deprecated and will be removed
# in the next major version. Please use exec-pre and exec-post
# as documented below instead.
# ************************************************************************
# Whether to copy the content of backup folder before creating the tar archive.
# In the rare scenario where the content of the source backup volume is continously
# updating, but we do not wish to stop the container while performing the backup,
# this setting can be used to ensure the integrity of the tar.gz file.

# BACKUP_FROM_SNAPSHOT="false"

# By default, the `/backup` directory inside the container will be backed up.
# In case you need to use a custom location, set `BACKUP_SOURCES`.

# BACKUP_SOURCES="/other/location"

# When given, all files in BACKUP_SOURCES whose full path matches the given
# regular expression will be excluded from the archive. Regular Expressions
# can be used as from the Go standard library https://pkg.go.dev/regexp

# BACKUP_EXCLUDE_REGEXP="\.log$"

########### BACKUP STORAGE

# The name of the remote bucket that should be used for storing backups. If
# this is not set, no remote backups will be stored.

# AWS_S3_BUCKET_NAME="backup-bucket"

# If you want to store the backup in a non-root location on your bucket
# you can provide a path. The path must not contain a leading slash.

# AWS_S3_PATH="my/backup/location"

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
# SSL certificates for AWS_ENDPOINT. You shouldn't use this unless you use
# self-signed certificates for your remote storage backend. This can only be
# used when AWS_ENDPOINT_PROTO is set to `https`.

# AWS_ENDPOINT_INSECURE="true"

# You can also backup files to any WebDAV server:

# The URL of the remote WebDAV server

# WEBDAV_URL="https://webdav.example.com"

# The Directory to place the backups to on the WebDAV server.
# If the path is not present on the server it will be created.

# WEBDAV_PATH="/my/directory/"

# The username for the WebDAV server

# WEBDAV_USERNAME="user"

# The password for the WebDAV server

# WEBDAV_PASSWORD="password"

# Setting this variable to `true` will disable verification of
# SSL certificates for WEBDAV_URL. You shouldn't use this unless you use
# self-signed certificates for your remote storage backend.

# WEBDAV_URL_INSECURE="true"

# You can also backup files to any SSH server:

# The URL of the remote SSH server

# SSH_HOST_NAME="server.local"

# The port of the remote SSH server
# Optional variable default value is `22`

# SSH_PORT=2222

# The Directory to place the backups to on the SSH server.

# SSH_REMOTE_PATH="/my/directory/"

# The username for the SSH server

# SSH_USER="user"

# The password for the SSH server

# SSH_PASSWORD="password"

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

########### EXECUTING COMMANDS IN CONTAINERS PRE/POST BACKUP

# It is possible to define commands to be run in any container before and after
# a backup is conducted. The commands themselves are defined in labels like
# `docker-volume-backup.exec-pre=/bin/sh -c 'mysqldump [options] > dump.sql'.
# Several options exist for controlling this feature:

# By default, any output of such a command is suppressed. If this value
# is configured to be "true", command execution output will be forwarded to
# the backup container's stdout and stderr.

# EXEC_FORWARD_OUTPUT="true"

# Without any further configuration, all commands defined in labels will be
# run before and after a backup. If you need more fine grained control, you
# can use this option to set a label that will be used for narrowing down
# the set of eligible containers. When set, an eligible container will also need
# to be labeled as `docker-volume-backup.exec-label=database`.

# EXEC_LABEL="database"

########### NOTIFICATIONS

# Notifications (email, Slack, etc.) can be sent out when a backup run finishes.
# Configuration is provided as a comma-separated list of URLs as consumed
# by `shoutrrr`: https://containrrr.dev/shoutrrr/v0.5/services/overview/
# The content of such notifications can be customized. Dedicated documentation
# on how to do this can be found in the README. When providing multiple URLs or
# an URL that contains a comma, the values can be URL encoded to avoid ambiguities.

# The below URL demonstrates how to send an email using the provided SMTP
# configuration and credentials.

# NOTIFICATION_URLS=smtp://username:password@host:587/?fromAddress=sender@example.com&toAddresses=recipient@example.com

# By default, notifications would only be sent out when a backup run fails
# To receive notifications for every run, set `NOTIFICATION_LEVEL` to `info`
# instead of the default `error`.

# NOTIFICATION_LEVEL="error"

########### DOCKER HOST

# If you are interfacing with Docker via TCP you can set the Docker host here
# instead of mounting the Docker socket as a volume. This is unset by default.

# DOCKER_HOST="tcp://docker_socket_proxy:2375"

########### LOCK_TIMEOUT

# In the case of overlapping cron schedules run by the same container,
# subsequent invocations will wait for previous runs to finish before starting.
# By default, this will time out and fail in case the lock could not be acquired
# after 60 minutes. In case you need to adjust this timeout, supply a duration
# value as per https://pkg.go.dev/time#ParseDuration to `LOCK_TIMEOUT`

# LOCK_TIMEOUT="60m"

########### EMAIL NOTIFICATIONS

# ************************************************************************
# Providing notification configuration like this has been deprecated
# and will be removed in the next major version. Please use NOTIFICATION_URLS
# as documented above instead.
# ************************************************************************

# In case SMTP credentials are provided, notification emails can be sent out when
# a backup run finished. These emails will contain the start time, the error
# message on failure and all prior log output.

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

In case you encouter double quoted values in your configuration you might be running an [older version of `docker-compose`][compose-issue].
You can work around this by either updating `docker-compose` or unquoting your configuration values.

[compose-issue]: https://github.com/docker/compose/issues/2854

## How to

### Stop containers during backup

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
    image: offen/docker-volume-backup:v2
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
    image: offen/docker-volume-backup:v2
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
    image: offen/docker-volume-backup:v2
    environment:
      # ... other configuration values go here
      NOTIFICATION_URLS=smtp://me:secret@smtp.example.com:587/?fromAddress=no-reply@example.com&toAddresses=you@example.com
```

Notification backends other than email are also supported.
Refer to the documentation of [shoutrrr][shoutrrr-docs] to find out about options and configuration.

[shoutrrr-docs]: https://containrrr.dev/shoutrrr/v0.5/services/overview/

### Customize notifications

The title and body of the notifications can be easily tailored to your needs using [go templates](https://pkg.go.dev/text/template).
Templates must be mounted inside the container in `/etc/dockervolumebackup/notifications.d/`: any file inside this directory will be parsed.

The files have to define [nested templates](https://pkg.go.dev/text/template#hdr-Nested_template_definitions) in order to override the original values. An example:
```
{{ define "title_success" -}}
✅ Successfully ran backup {{ .Config.BackupStopContainerLabel }}
{{- end }}

{{ define "body_success" -}}
▶️ Start time: {{ .Stats.StartTime | formatTime }}
⏹️ End time: {{ .Stats.EndTime | formatTime }}
⌛ Took time: {{ .Stats.TookTime }}
🛑 Stopped containers: {{ .Stats.Containers.Stopped }}/{{ .Stats.Containers.All }} ({{ .Stats.Containers.StopErrors }} errors)
⚖️ Backup size: {{ .Stats.BackupFile.Size | formatBytesBin }} / {{ .Stats.BackupFile.Size | formatBytesDec }}
🗑️ Pruned backups: {{ .Stats.Storages.Local.Pruned }}/{{ .Stats.Storages.Local.Total }} ({{ .Stats.Storages.Local.PruneErrors }} errors)
{{- end }}
```

Overridable template names are: `title_success`, `body_success`, `title_failure`, `body_failure`.

For a full list of available variables and functions, see [this page](https://github.com/offen/docker-volume-backup/blob/master/docs/NOTIFICATION-TEMPLATES.md).

### Run custom commands before / after backup

In certain scenarios it can be required to run specific commands before and after a backup is taken (e.g. dumping a database).
When mounting the Docker socket into the `docker-volume-backup` container, you can define pre- and post-commands that will be run in the context of the target container.
Such commands are defined by specifying the command in a `docker-volume-backup.exec-[pre|post]` label.

Taking a database dump using `mysqldump` would look like this:

```yml
version: '3'

services:
  # ... define other services using the `data` volume here
  database:
    image: mariadb
    volumes:
      - backup_data:/tmp/backups
    labels:
      - docker-volume-backup.exec-pre=/bin/sh -c 'mysqldump --all-databases > /backups/dump.sql'

volumes:
  backup_data:
```

Due to Docker limitations, you currently cannot use any kind of redirection in these commands unless you pass the command to `/bin/sh -c` or similar.
I.e. instead of using `echo "ok" > ok.txt` you will need to use `/bin/sh -c 'echo "ok" > ok.txt'`.

If you need fine grained control about which container's commands are run, you can use the `EXEC_LABEL` configuration on your `docker-volume-backup` container:

```yml
version: '3'

services:
  database:
    image: mariadb
    volumes:
      - backup_data:/tmp/backups
    labels:
      - docker-volume-backup.exec-pre=/bin/sh -c 'mysqldump --all-databases > /tmp/volume/dump.sql'
      - docker-volume-backup.exec-label=database

  backup:
    image: offen/docker-volume-backup:v2
    environment:
      EXEC_LABEL: database
    volumes:
      - data:/backup/dump:ro
      - /var/run/docker.sock:/var/run/docker.sock:ro

volumes:
  backup_data:
```


The backup procedure is guaranteed to wait for all `pre` commands to finish.
However there are no guarantees about the order in which they are run, which could also happen concurrently.

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
- Using a temporary once-off container, mount the volume (the example assumes it's named `data`) and copy over the backup. Make sure you copy the correct path level (this depends on how you mount your volume into the backup container), you might need to strip some leading elements
  ```console
  docker run -d --name temp_restore_container -v data:/backup_restore alpine
  docker cp /tmp/backup/data-backup temp_restore_container:/backup_restore
  docker stop temp_restore_container
  docker rm temp_restore_container
  ```
- Restart the container(s) that are using the volume

Depending on your setup and the application(s) you are running, this might involve other steps to be taken still.

---

If you want to rollback an entire volume to an earlier backup snapshot (recommended for database volumes):

- Trigger a manual backup if necessary (see `Manually triggering a backup`).
- Stop the container(s) that are using the volume.
- If volume was initially created using docker-compose, find out exact volume name using:
  ```console
  docker volume ls
  ```
- Remove existing volume (the example assumes it's named `data`):
  ```console
  docker volume rm data
  ```
- Create new volume with the same name and restore a snapshot:
  ```console
  docker run --rm -it -v data:/backup/my-app-backup -v /path/to/local_backups:/archive:ro alpine tar -xvzf /archive/full_backup_filename.tar.gz
  ```
- Restart the container(s) that are using the volume.

### Set the timezone the container runs in

By default a container based on this image will run in the UTC timezone.
As the image is designed to be as small as possible, additional timezone data is not included.
In case you want to run your cron rules in your local timezone (respecting DST and similar), you can mount your Docker host's `/etc/timezone` and `/etc/localtime` in read-only mode:

```yml
version: '3'

services:
  backup:
    image: offen/docker-volume-backup:v2
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
    image: offen/docker-volume-backup:v2
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

### Update deprecated email configuration

Starting with version 2.6.0, configuring email notifications using `EMAIL_*` keys has been deprecated.
Instead of providing multiple values using multiple keys, you can now provide a single URL for `NOTIFICATION_URLS`.

Before:
```ini
EMAIL_NOTIFICATION_RECIPIENT="you@example.com"
EMAIL_NOTIFICATION_SENDER="no-reply@example.com"
EMAIL_SMTP_HOST="posteo.de"
EMAIL_SMTP_PASSWORD="secret"
EMAIL_SMTP_USERNAME="me"
EMAIL_SMTP_PORT="587"
```

After:
```ini
NOTIFICATION_URLS=smtp://me:secret@posteo.de:587/?fromAddress=no-reply@example.com&toAddresses=you@example.com
```

### Replace deprecated `BACKUP_FROM_SNAPSHOT` usage

Starting with version 2.15.0, the `BACKUP_FROM_SNAPSHOT` feature has been deprecated.
If you need to prepare your sources before the backup is taken, use `exec-pre`, `exec-post` and an intermediate volume:

```yml
version: '3'

services:
  my_app:
    build: .
    volumes:
      - data:/var/my_app
      - backup:/tmp/backup
    labels:
      - docker-volume-backup.exec-pre=cp -r /var/my_app /tmp/backup/my-app
      - docker-volume-backup.exec-post=rm -rf /tmp/backup/my-app

  backup:
    image: offen/docker-volume-backup:latest
    environment:
      BACKUP_SOURCES: /tmp/backup
    volumes:
      - backup:/backup:ro
      - /var/run/docker.sock:/var/run/docker.sock:ro

volumes:
  data:
  backup:
```

### Using a custom Docker host

If you are interfacing with Docker via TCP, set `DOCKER_HOST` to the correct URL.
```ini
DOCKER_HOST=tcp://docker_socket_proxy:2375
```

In case you are using a socket proxy, it must support `GET` and `POST` requests to the `/containers` endpoint. If you are using Docker Swarm, it must also support the `/services` endpoint. If you are using pre/post backup commands, it must also support the `/exec` endpoint.

### Run multiple backup schedules in the same container

Multiple backup schedules with different configuration can be configured by mounting an arbitrary number of configuration files (using the `.env` format) into `/etc/dockervolumebackup/conf.d`:

```yml
version: '3'

services:
  # ... define other services using the `data` volume here
  backup:
    image: offen/docker-volume-backup:v2
    volumes:
      - data:/backup/my-app-backup:ro
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - ./configuration:/etc/dockervolumebackup/conf.d

volumes:
  data:
```

A separate cronjob will be created for each config file.
If a configuration value is set both in the global environment as well as in the config file, the config file will take precedence.
The `backup` command expects to run on an exclusive lock, so in case you provide the same or overlapping schedules in your cron expressions, the runs will still be executed serially, one after the other.
The exact order of schedules that use the same cron expression is not specified.
In case you need your schedules to overlap, you need to create a dedicated container for each schedule instead.
When changing the configuration, you currently need to manually restart the container for the changes to take effect.

### Define different retention schedules

If you want to manage backup retention on different schedules, the most straight forward approach is to define a dedicated configuration for retention rule using a different prefix in the `BACKUP_FILENAME` parameter and then run them on different cron schedules.

For example, if you wanted to keep daily backups for 7 days, weekly backups for a month, and retain monthly backups forever, you could create three configuration files and mount them into `/etc/dockervolumebackup.d`:

```ini
# 01daily.conf
BACKUP_FILENAME="daily-backup-%Y-%m-%dT%H-%M-%S.tar.gz"
# run every day at 2am
BACKUP_CRON_EXPRESSION="0 2 * * *"
BACKUP_PRUNING_PREFIX="daily-backup-"
BACKUP_RETENTION_DAYS="7"
```

```ini
# 02weekly.conf
BACKUP_FILENAME="weekly-backup-%Y-%m-%dT%H-%M-%S.tar.gz"
# run every monday at 3am
BACKUP_CRON_EXPRESSION="0 3 * * 1"
BACKUP_PRUNING_PREFIX="weekly-backup-"
BACKUP_RETENTION_DAYS="31"
```

```ini
# 03monthly.conf
BACKUP_FILENAME="monthly-backup-%Y-%m-%dT%H-%M-%S.tar.gz"
# run every 1st of a month at 4am
BACKUP_CRON_EXPRESSION="0 4 1 * *"
```

Note that while it's possible to define colliding cron schedules for each of these configurations, you might need to adjust the value for `LOCK_TIMEOUT` in case your backups are large and might take longer than an hour.

## Recipes

This section lists configuration for some real-world use cases that you can mix and match according to your needs.

### Backing up to AWS S3

```yml
version: '3'

services:
  # ... define other services using the `data` volume here
  backup:
    image: offen/docker-volume-backup:v2
    environment:
      AWS_S3_BUCKET_NAME: backup-bucket
      AWS_ACCESS_KEY_ID: AKIAIOSFODNN7EXAMPLE
      AWS_SECRET_ACCESS_KEY: wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY
    volumes:
      - data:/backup/my-app-backup:ro
      - /var/run/docker.sock:/var/run/docker.sock:ro

volumes:
  data:
```

### Backing up to Filebase

```yml
version: '3'

services:
  # ... define other services using the `data` volume here
  backup:
    image: offen/docker-volume-backup:v2
    environment:
      AWS_ENDPOINT: s3.filebase.com
      AWS_S3_BUCKET_NAME: filebase-bucket
      AWS_ACCESS_KEY_ID: FILEBASE-ACCESS-KEY
      AWS_SECRET_ACCESS_KEY: FILEBASE-SECRET-KEY
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
    image: offen/docker-volume-backup:v2
    environment:
      AWS_ENDPOINT: minio.example.com
      AWS_S3_BUCKET_NAME: backup-bucket
      AWS_ACCESS_KEY_ID: MINIOACCESSKEY
      AWS_SECRET_ACCESS_KEY: MINIOSECRETKEY
    volumes:
      - data:/backup/my-app-backup:ro
      - /var/run/docker.sock:/var/run/docker.sock:ro

volumes:
  data:
```

### Backing up to WebDAV

```yml
version: '3'

services:
  # ... define other services using the `data` volume here
  backup:
    image: offen/docker-volume-backup:v2
    environment:
      WEBDAV_URL: https://webdav.mydomain.me
      WEBDAV_PATH: /my/directory/
      WEBDAV_USERNAME: user
      WEBDAV_PASSWORD: password
    volumes:
      - data:/backup/my-app-backup:ro
      - /var/run/docker.sock:/var/run/docker.sock:ro

volumes:
  data:
```

### Backing up to SSH

```yml
version: '3'

services:
  # ... define other services using the `data` volume here
  backup:
    image: offen/docker-volume-backup:v2
    environment:
      SSH_HOST_NAME: server.local
      SSH_PORT: 2222
      SSH_USER: user
      SSH_PASSWORD: password
      SSH_REMOTE_PATH: /data
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
    image: offen/docker-volume-backup:v2
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
    image: offen/docker-volume-backup:v2
    environment:
      AWS_S3_BUCKET_NAME: backup-bucket
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
    image: offen/docker-volume-backup:v2
    environment:
      # take a backup on every hour
      BACKUP_CRON_EXPRESSION: "0 * * * *"
      AWS_S3_BUCKET_NAME: backup-bucket
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
    image: offen/docker-volume-backup:v2
    environment:
      AWS_S3_BUCKET_NAME: backup-bucket
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
    image: offen/docker-volume-backup:v2
    environment:
      AWS_S3_BUCKET_NAME: backup-bucket
      AWS_ACCESS_KEY_ID: AKIAIOSFODNN7EXAMPLE
      AWS_SECRET_ACCESS_KEY: wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY
      GPG_PASSPHRASE: somesecretstring
    volumes:
      - data:/backup/my-app-backup:ro
      - /var/run/docker.sock:/var/run/docker.sock:ro

volumes:
  data:
```

### Using mysqldump to prepare the backup

```yml
version: '3'

services:
  database:
    image: mariadb:latest
    labels:
      - docker-volume-backup.exec-pre=/bin/sh -c 'mysqldump -psecret --all-databases > /tmp/dumps/dump.sql'
    volumes:
      - app_data:/tmp/dumps
  backup:
    image: offen/docker-volume-backup:v2
    environment:
      BACKUP_FILENAME: db.tar.gz
      BACKUP_CRON_EXPRESSION: "0 2 * * *"
    volumes:
      - ./local:/archive
      - data:/backup/data:ro
      - /var/run/docker.sock:/var/run/docker.sock

volumes:
  data:
```

### Running multiple instances in the same setup

```yml
version: '3'

services:
  # ... define other services using the `data_1` and `data_2` volumes here
  backup_1: &backup_service
    image: offen/docker-volume-backup:v2
    environment: &backup_environment
      BACKUP_CRON_EXPRESSION: "0 2 * * *"
      AWS_S3_BUCKET_NAME: backup-bucket
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
- The original image uses a shell script, when this version is written in Go.
- The original image proposed to handle backup rotation through AWS S3 lifecycle policies.
This image adds the option to rotate away old backups through the same command so this functionality can also be offered for non-AWS storage backends like MinIO.
Local copies of backups can also be pruned once they reach a certain age.
- InfluxDB specific functionality from the original image was removed.
- `arm64` and `arm/v7` architectures are supported.
- Docker in Swarm mode is supported.
- Notifications on finished backups are supported.
- IAM authentication through instance profiles is supported.
