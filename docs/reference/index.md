---
title: Configuration Reference
layout: default
nav_order: 2
---

# Configuration reference

Backup targets, schedule and retention are configured using environment variables.

{: .note }
You can use any environment variable from below also with a `_FILE` suffix to be able to load the value from a file.
This is typically useful when using [Docker Secrets](https://docs.docker.com/engine/swarm/secrets/) or similar.
Note that secrets will not be trimmed of leading or trailing whitespace.

{: .warning }
In case you encounter double quoted values in your runtime configuration you might still be using an [older version of `docker-compose`][compose-issue].
You can work around this by either updating `docker-compose` or unquoting your configuration values.

You can populate below template according to your requirements and use it as your `env_file`:

{% raw %}
```
########### BACKUP SCHEDULE

# Backups run on the given cron schedule in `busybox` flavor. If no
# value is set, `@daily` will be used. If you do not want the cron
# to ever run, use `0 0 5 31 2 ?`.

# BACKUP_CRON_EXPRESSION="0 2 * * *"

# The compression algorithm used in conjunction with tar.
# Valid options are: "gz" (Gzip) and "zst" (Zstd).
# Note that the selection affects the file extension.

# BACKUP_COMPRESSION="gz"

# Parallelism level for "gz" (Gzip) compression.
# Defines how many blocks of data are concurrently processed.
# Higher values result in faster compression. No effect on decompression
# Default = 1. Setting this to 0 will use all available threads.

# GZIP_PARALLELISM=1

# The name of the backup file including the extension.
# Format verbs will be replaced as in `strftime`. Omitting them
# will result in the same filename for every backup run, which means previous
# versions will be overwritten on subsequent runs.
# Extension can be defined literally or via "{{ .Extension }}" template,
# in which case it will become either "tar.gz" or "tar.zst" (depending
# on your BACKUP_COMPRESSION setting).
# The default results in filenames like: `backup-2021-08-29T04-00-00.tar.gz`.

# BACKUP_FILENAME="backup-%Y-%m-%dT%H-%M-%S.{{ .Extension }}"

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
# In the rare scenario where the content of the source backup volume is continuously
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

# Exclude one or many storage backends from the pruning process.
# E.g. with one backend excluded: BACKUP_SKIP_BACKENDS_FROM_PRUNE=s3
# E.g. with multiple backends excluded: BACKUP_SKIP_BACKENDS_FROM_PRUNE=s3,webdav
# Available backends are: S3, WebDAV, SSH, Local, Dropbox, Azure
# Note: The name of the backends is case insensitive. 
# Default: All backends get pruned.

# BACKUP_SKIP_BACKENDS_FROM_PRUNE=

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

# If you wish to use self signed certificates your S3 server, you can pass
# the location of a PEM encoded CA certificate and it will be used for
# validating your certificates.
# Alternatively, pass a PEM encoded string containing the certificate.

# AWS_ENDPOINT_CA_CERT="/path/to/cert.pem"

# Setting this variable will change the S3 storage class header.
# Defaults to "STANDARD", you can set this value according to your needs.

# AWS_STORAGE_CLASS="GLACIER"

# Setting this variable will change the S3 default part size for the copy step.
# This value is useful when you want to upload large files.
# NB : While using Scaleway as S3 provider, be aware that the parts counter is set to 1.000.
# While Minio uses a hard coded value to 10.000. As a workaround, try to set a higher value.
# Defaults to "16" (MB) if unset (from minio), you can set this value according to your needs.
# The unit is in MB and an integer.

# AWS_PART_SIZE=16

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

# The private key path in container for SSH server
# Default value: /root/.ssh/id_rsa
# If file is mounted to /root/.ssh/id_rsa path it will be used. Non-RSA keys will
# also work.

# SSH_IDENTITY_FILE="/root/.ssh/id_rsa"

# The passphrase for the identity file

# SSH_IDENTITY_PASSPHRASE="pass"

# The credential's account name when using Azure Blob Storage. This has to be
# set when using Azure Blob Storage.

# AZURE_STORAGE_ACCOUNT_NAME="account-name"

# The credential's primary account key when using Azure Blob Storage. If this
# is not given, the command tries to fall back to using a managed identity.

# AZURE_STORAGE_PRIMARY_ACCOUNT_KEY="<xxx>"

# The container name when using Azure Blob Storage.

# AZURE_STORAGE_CONTAINER_NAME="container-name"

# The service endpoint when using Azure Blob Storage. This is a template that
# can be passed the account name as shown in the default value below.

# AZURE_STORAGE_ENDPOINT="https://{{ .AccountName }}.blob.core.windows.net/"

# Absolute remote path in your Dropbox where the backups shall be stored.
# Note: Use your app's subpath in Dropbox, if it doesn't have global access.
# Consulte the README for further information.

# DROPBOX_REMOTE_PATH="/my/directory"

# Number of concurrent chunked uploads for Dropbox.
# Values above 6 usually result in no enhancements.

# DROPBOX_CONCURRENCY_LEVEL="6"

# App key and app secret from your app created at https://www.dropbox.com/developers/apps/info

# DROPBOX_APP_KEY=""
# DROPBOX_APP_SECRET=""

# Refresh token to request new short-lived tokens (OAuth2). Consult README to see how to get one.

# DROPBOX_REFRESH_TOKEN=""

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

# When trying to scale down Docker Swarm services, give up after
# the specified amount of time in case the service has not converged yet.
# In case you need to adjust this timeout, supply a duration
# value as per https://pkg.go.dev/time#ParseDuration to `BACKUP_STOP_SERVICE_TIMEOUT`.
# Defaults to 5 minutes.

# BACKUP_STOP_SERVICE_TIMEOUT="5m"

########### EXECUTING COMMANDS IN CONTAINERS PRE/POST BACKUP

# It is possible to define commands to be run in any container before and after
# a backup is conducted. The commands themselves are defined in labels like
# `docker-volume-backup.archive-pre=/bin/sh -c 'mysqldump [options] > dump.sql'.
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
# by `shoutrrr`: https://containrrr.dev/shoutrrr/0.7/services/overview/
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
# of addresses if you want to notify multiple recipients. If this is
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
{% endraw %}

[compose-issue]: https://github.com/docker/compose/issues/2854
