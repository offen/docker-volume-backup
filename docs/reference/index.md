---
title: Configuration Reference
layout: default
nav_order: 2
---

# Configuration reference

Backup targets, schedule and retention are configured using environment variables.

{: .note }
As per established convention, you can use any environment variable key from below with a `_FILE` suffix in order to load the value from a file instead.
This is typically useful when using [Docker Secrets](https://docs.docker.com/engine/swarm/secrets/) or similar.
Note that secrets will not be trimmed of leading or trailing whitespace.

{: .warning }
In case you encounter double quoted values in your runtime configuration you might still be using an [older version of `docker-compose`][compose-issue].
You can work around this by either updating `docker-compose` or unquoting your configuration values.

You can populate below template according to your requirements and use it as your `env_file`.
The values for each key currently match its default.

{% raw %}
```
########### BACKUP SCHEDULE

# Backups can be run on fixed scheduled that are defined as a cron expression.
# A cron expression represents a set of times, using 5 or 6 space-separated fields.
#
# Field name   | Mandatory? | Allowed values  | Allowed special characters
# ----------   | ---------- | --------------  | --------------------------
# Seconds      | No         | 0-59            | * / , -
# Minutes      | Yes        | 0-59            | * / , -
# Hours        | Yes        | 0-23            | * / , -
# Day of month | Yes        | 1-31            | * / , - ?
# Month        | Yes        | 1-12 or JAN-DEC | * / , -
# Day of week  | Yes        | 0-6 or SUN-SAT  | * / , - ?
#
# Month and Day-of-week field values are case insensitive.
# "SUN", "Sun", and "sun" are equally accepted.
# If you do not want the cron to ever run, use `0 0 5 31 2 ?`.
# Refer to sites like <https://crontab.guru> for help.
# If no value is set, `@daily` will be used, which runs every
# day at midnight.

# BACKUP_CRON_EXPRESSION="@daily"

# ---

# The compression algorithm used in conjunction with tar.
# Valid options are: "gz" (Gzip), "zst" (Zstd) or "none" (tar only).
# Default is "gz". Note that the selection affects the file extension.

# BACKUP_COMPRESSION="gz"

# ---

# Parallelism level for "gz" (Gzip) compression.
# Defines how many blocks of data are concurrently processed.
# Higher values result in faster compression. No effect on decompression
# Default = 1. Setting this to 0 will use all available threads.

# GZIP_PARALLELISM="1"

# ---

# The desired name of the backup file including the extension.
# Format verbs will be replaced as in `strftime`. Omitting all verbs
# will result in the same filename for every backup run, which means previous
# versions will be overwritten on subsequent runs.
# Extension can be defined literally or via "{{ .Extension }}" template,
# in which case it will become either "tar.gz", "tar.zst" or ".tar" (depending
# on your BACKUP_COMPRESSION setting).
# The default results in filenames like: `backup-2021-08-29T04-00-00.tar.gz`.

# BACKUP_FILENAME="backup-%Y-%m-%dT%H-%M-%S.{{ .Extension }}"

# ---

# Setting BACKUP_FILENAME_EXPAND to true allows for environment variable
# placeholders in BACKUP_FILENAME, BACKUP_LATEST_SYMLINK and in
# BACKUP_PRUNING_PREFIX that will get expanded at runtime,
# e.g. `backup-$HOSTNAME-%Y-%m-%dT%H-%M-%S.tar.gz`. Expansion happens before
# interpolating strftime tokens. It is disabled by default.
# Please note that you will need to escape the `$` when providing the value
# in a docker-compose.yml file, i.e. using $$VAR instead of $VAR.

# BACKUP_FILENAME_EXPAND="true"

# ---

# When storing local backups, a symlink to the latest backup can be created
# in case a value is given for this key. This has no effect on remote backups.
# Example: "backup.latest.tar.gz"

# BACKUP_LATEST_SYMLINK=""

# ---

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

# ---

# By default, the contents of the `/backup` directory inside the container
# will be backed up. In case you need to use a custom location, set `BACKUP_SOURCES`.
# Example: "/other/location"

# BACKUP_SOURCES="/backup"

# ---

# When a value is given, all files in BACKUP_SOURCES whose full path matches the
# regular expression will be excluded from the archive. Regular Expressions
# can be used as from the Go standard library https://pkg.go.dev/regexp
# Example: "\.log$"

# BACKUP_EXCLUDE_REGEXP=""

# ---

# Exclude one or many storage backends from the pruning process.
# Available backends are: S3, WebDAV, SSH, Local, Dropbox, Azure
# E.g. with one backend excluded: BACKUP_SKIP_BACKENDS_FROM_PRUNE=s3
# E.g. with multiple backends excluded: BACKUP_SKIP_BACKENDS_FROM_PRUNE=s3,webdav
# Note: The names of the backends are case insensitive. 
# Default: All backends get pruned.

# BACKUP_SKIP_BACKENDS_FROM_PRUNE=""

########### S3 COMPATIBLE STORAGE

# The name of the remote bucket that should be used for storing backups. If
# this is not set, no remote backups will be stored.
# Example: "backup-bucket"

# AWS_S3_BUCKET_NAME=""

# ---

# If you want to store the backup in a non-root location on your bucket
# you can provide a path. The path must not contain a leading slash.
# Example: "my/backup/location"

# AWS_S3_PATH=""

# ---

# Define credentials for authenticating against the backup storage and a bucket
# name. Although all of these keys are `AWS`-prefixed, the setup can be used
# with any S3 compatible storage.

# AWS_ACCESS_KEY_ID=""
# AWS_SECRET_ACCESS_KEY=""

# ---

# Instead of providing static credentials, you can also use IAM instance profiles
# or similar to provide authentication. Some possible configuration options on AWS:
# - EC2: http://169.254.169.254
# - ECS: http://169.254.170.2

# AWS_IAM_ROLE_ENDPOINT=""

# ---

# This is the FQDN of your storage server, e.g. `storage.example.com`.
# If you need to set a specific (non-https) protocol, you will need to use the option below.
# The default value points to the standard AWS S3 endpoint.

# AWS_ENDPOINT="s3.amazonaws.com"

# ---

# The protocol to be used when communicating with your S3 storage server.
# Defaults to "https". You can set this to "http" when communicating with
# a different Docker container in the same virtual network for example.

# AWS_ENDPOINT_PROTO="https"

# ---

# Setting this variable to `true` will disable verification of
# SSL certificates for AWS_ENDPOINT. You shouldn't use this unless you use
# self-signed certificates for your remote storage backend. This can only be
# used when AWS_ENDPOINT_PROTO is set to `https`.

# AWS_ENDPOINT_INSECURE="false"

# ---

# If you wish to use self signed certificates your S3 server, you can pass
# the location of a PEM encoded CA certificate and it will be used for
# validating your certificates. Alternatively, pass a PEM encoded string
# containing the certificate.
# Example: "/path/to/cert.pem"

# AWS_ENDPOINT_CA_CERT=""

# ---

# Setting a value for this key will change the S3 storage class header.
# Default behavior is to use the standard class when no value is given.
# Example: "GLACIER"

# AWS_STORAGE_CLASS=""

# ---

# Setting this variable will change the S3 default part size for the copy step.
# This value is useful when you want to upload large files.
# NB: While using Scaleway as S3 provider, be aware that the parts counter is set to 1.000.
# While Minio uses a hard coded value to 10.000. As a workaround, try to set a higher value.
# Defaults to "16" (MB) if unset (from minio), you can set this value according to your needs.
# The unit is in MB and an integer.

# AWS_PART_SIZE="16"

########### WEBDAV STORAGE

# The URL of the remote WebDAV server
# Example: "https://webdav.example.com"

# WEBDAV_URL=""

# ---

# The Directory to place the backups to on the WebDAV server.
# If the path is not present on the server it will be created.
# Example: "/my/directory/"

# WEBDAV_PATH=""

# ---

# The username for the WebDAV server
# Example: "user"

# WEBDAV_USERNAME=""

# ---

# The password for the WebDAV server
# Example: "password"

# WEBDAV_PASSWORD=""

# ---

# Setting this variable to "true" will disable verification of
# SSL certificates for WEBDAV_URL. You shouldn't use this unless you use
# self-signed certificates for your remote storage backend.

# WEBDAV_URL_INSECURE="false"

########### SSH/SFTP STORAGE

# The FQDN of the remote SSH server
# Example: "server.local"

# SSH_HOST_NAME=""

# ---

# The port of the remote SSH server

# SSH_PORT="22"

# ---

# The Directory to place the backups to on the SSH server.
# Example: "/home/user/backups"

# SSH_REMOTE_PATH=""

# ---

# The username for the SSH server
# Example: "user"

# SSH_USER=""

# ---

# The password for the SSH server
# Example: "password"

# SSH_PASSWORD=""

# ---

# The private key path in container for SSH server.
# Consumers can mount a file into /root/.ssh/id_rsa (or the respective value)
# in order to have it being used. Non-RSA keys (e.g. ed25519) will also work.

# SSH_IDENTITY_FILE="/root/.ssh/id_rsa"

# ---

# The passphrase for the identity file if applicable
# Example: "pass"

# SSH_IDENTITY_PASSPHRASE=""

########### AZURE BLOB STORAGE

# The credential's account name when using Azure Blob Storage. This has to be
# set when using Azure Blob Storage.
# Example: "account-name"

# AZURE_STORAGE_ACCOUNT_NAME=""

# ---

# The credential's primary account key when using Azure Blob Storage. If this
# is not given, the command tries to fall back to using a connection string
# (if given) or a managed identity (if neither is set).

# AZURE_STORAGE_PRIMARY_ACCOUNT_KEY=""

# ---

# A connection string for accessing Azure Blob Storage. If this
# is not given, the command tries to fall back to using a primary account key
# (if given) or a managed identity (if neither is set).

# AZURE_STORAGE_CONNECTION_STRING=""

# ---

# The container name when using Azure Blob Storage.
# Example: "container-name"

# AZURE_STORAGE_CONTAINER_NAME=""

# ---

# The service endpoint when using Azure Blob Storage. This is a template that
# can be passed the account name as shown in the default value below.

# AZURE_STORAGE_ENDPOINT="https://{{ .AccountName }}.blob.core.windows.net/"

# ---

# The access tier when using Azure Blob Storage. Possible values are
# https://github.com/Azure/azure-sdk-for-go/blob/sdk/storage/azblob/v1.3.2/sdk/storage/azblob/internal/generated/zz_constants.go#L14-L30
# Example: "Cold"

# AZURE_STORAGE_ACCESS_TIER=""

########### DROPBOX STORAGE

# Absolute remote path in your Dropbox where the backups shall be stored.
# Note: Use your app's subpath in Dropbox, if it doesn't have global access.
# Consult the README for further information.
# Example: "/my/directory"

# DROPBOX_REMOTE_PATH=""

# ---

# App key and app secret from your app created at https://www.dropbox.com/developers/apps

# DROPBOX_APP_KEY=""
# DROPBOX_APP_SECRET=""

# ---

# Number of concurrent chunked uploads for Dropbox.
# Values above 6 usually result in no enhancements.

# DROPBOX_CONCURRENCY_LEVEL="6"

# ---

# Refresh token to request new short-lived tokens (OAuth2). Consult README to see how to get one.

# DROPBOX_REFRESH_TOKEN=""

########### GOOGLE DRIVE STORAGE

# The JSON credentials for a Google service account with access to Google Drive.
# You can provide either:
# 1. The actual JSON content directly
# 2. Use the _FILE suffix to load from a file (e.g., GOOGLE_DRIVE_CREDENTIALS_JSON_FILE)
#
# Examples:
# Option 1 - JSON content:
# docker run [...] \
#    -e GOOGLE_DRIVE_CREDENTIALS_JSON='{"type":"service_account",...}'
#
# Option 2 - Using _FILE suffix (recommended for Docker Secrets):
# docker run [...] \
#    -v ./credentials.json:/creds/google-credentials.json \
#    -e GOOGLE_DRIVE_CREDENTIALS_JSON_FILE=/creds/google-credentials.json
#
# GOOGLE_DRIVE_CREDENTIALS_JSON=""

# ---

# The ID of the Google Drive folder where backups will be uploaded.
# You can find the folder ID in the URL when viewing the folder in Google Drive.
# 
# Example: "1A2B3C4D5E6F7G8H9I0J"
#
# GOOGLE_DRIVE_FOLDER_ID=""

# ---

# The email address of the user to impersonate when accessing Google Drive (domain-wide delegation).
# This is required becasue your service account needs to act on behalf of a user in your organization in order to upload files.
# How to: https://support.google.com/a/answer/162106
# Example: "user@example.com"
#
# GOOGLE_DRIVE_IMPERSONATE_SUBJECT=""

# ---

# (Optional) Custom Google Drive API endpoint. This is primarily for testing with a mock server.
# Example: "http://localhost:8080/drive/v3"
#
# GOOGLE_DRIVE_ENDPOINT=""

# ---

# (Optional) Custom token URL for Google Drive authentication. This is primarily for testing with a mock server.
# Example: "http://localhost:8080/token"
#
# GOOGLE_DRIVE_TOKEN_URL=""

########### LOCAL FILE STORAGE

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

# Pass zero or a positive integer value to enable automatic rotation of
# old backups. The value declares the number of days for which a backup is kept.

# BACKUP_RETENTION_DAYS="-1"

# ---

# In case the duration a backup takes fluctuates noticeably in your setup
# you can adjust this setting to make sure there are no race conditions
# between the backup finishing and the rotation not deleting backups that
# sit on the edge of the time window. Set this value to a duration
# that is expected to be bigger than the maximum difference of backups.
# Valid values have a suffix of (s)econds, (m)inutes or (h)ours. By default,
# one minute is used.

# BACKUP_PRUNING_LEEWAY="1m"

# ---

# In case your target bucket or directory contains other files than the ones
# managed by this container, you can limit the scope of rotation by setting
# a prefix value. This would usually be the non-parametrized part of your
# BACKUP_FILENAME. E.g. if BACKUP_FILENAME is `db-backup-%Y-%m-%dT%H-%M-%S.tar.gz`,
# you can set BACKUP_PRUNING_PREFIX to `db-backup-` and make sure
# unrelated files are not affected by the rotation mechanism.

# BACKUP_PRUNING_PREFIX=""

########### BACKUP ENCRYPTION

# All of the encryption options are mutually exclusive. Provide a single option
# for the encryption scheme of your choice.

# Backups can be encrypted symmetrically using gpg in case a passphrase is given.

# GPG_PASSPHRASE=""

# ---

# Backups can be encrypted asymmetrically using gpg in case publickeys are given.
# You can use pipe syntax to pass a multiline value.

# GPG_PUBLIC_KEY_RING=""

# ---

# Backups can be encrypted symmetrically using age in case a passphrase is given.

# AGE_PASSPHRASE=""

# ---

# Backups can be encrypted asymmetrically using age in case publickeys are given.
# Multiple keys need to be provided as a comma separated list. Right now, this
# supports `age` and `ssh` keys

# AGE_PUBLIC_KEYS=""

########### STOPPING CONTAINERS AND SERVICES DURING BACKUP

# Containers or services can be stopped by applying a
# `docker-volume-backup.stop-during-backup` label. By default, all containers and
# services that are labeled with `true` will be stopped. If you need more fine
# grained control (e.g. when running multiple containers based on this image),
# you can override this default by specifying a different string value here.
# BACKUP_STOP_DURING_BACKUP_LABEL="true"

# When trying to scale down Docker Swarm services, give up after
# the specified amount of time in case the service has not converged yet.
# In case you need to adjust this timeout, supply a duration
# value as per https://pkg.go.dev/time#ParseDuration to `BACKUP_STOP_SERVICE_TIMEOUT`.

# BACKUP_STOP_SERVICE_TIMEOUT="5m"

########### EXECUTING COMMANDS IN CONTAINERS DURING THE BACKUP LIFECYCLE

# It is possible to define commands to be run in any container before and after
# a backup is conducted. The commands themselves are defined in labels like
# `docker-volume-backup.archive-pre=/bin/sh -c 'mysqldump [options] > dump.sql'.
# Several options exist for controlling this feature:

# By default, any output of such a command is suppressed. If this value
# is configured to be "true", command execution output will be forwarded to
# the backup container's stdout and stderr.

# EXEC_FORWARD_OUTPUT="false"

# ---

# Without any further configuration, all commands defined in labels will be
# run before and after a backup. If you need more fine grained control, you
# can use this option to set a label that will be used for narrowing down
# the set of eligible containers. E.g. when setting this to `database`,
# an eligible container will also need to be labeled as `docker-volume-backup.exec-label=database`.

# EXEC_LABEL=""

########### NOTIFICATIONS

# Notifications (email, Slack, etc.) can be sent out when a backup run finishes.
# Configuration is provided as a comma-separated list of URLs as consumed
# by `shoutrrr`: https://containrrr.dev/shoutrrr/v0.8/services/overview/
# The content of such notifications can be customized. Dedicated documentation
# on how to do this can be found in the README. When providing multiple URLs or
# an URL that contains a comma, the values can be URL encoded to avoid ambiguities.

# The following example URL demonstrates how to send an email using the provided SMTP
# configuration and credentials.
# Example: "smtp://username:password@host:587/?fromAddress=sender@example.com&toAddresses=recipient@example.com"

# NOTIFICATION_URLS=""

# ---

# By default, notifications would only be sent out when a backup run fails
# To receive notifications for every run, set `NOTIFICATION_LEVEL` to `info`
# instead of the default `error`.

# NOTIFICATION_LEVEL="error"

########### DOCKER HOST

# If you are interfacing with Docker via TCP you can set the Docker host here
# instead of mounting the Docker socket as a volume. This is unset by default.
# Example: "tcp://docker_socket_proxy:2375"

# DOCKER_HOST=""

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
# Example: "you@example.com"

# EMAIL_NOTIFICATION_RECIPIENT=""

# ---

# The "From" header of the sent email.
# Example: "no-reply@example.com"

# EMAIL_NOTIFICATION_SENDER="noreply@nohost"

# ---

# Configuration and credentials for the SMTP server to be used.

# EMAIL_SMTP_HOST=""
# EMAIL_SMTP_PASSWORD=""
# EMAIL_SMTP_USERNAME=""
# EMAIL_SMTP_PORT="587"
```
{% endraw %}

[compose-issue]: https://github.com/docker/compose/issues/2854
