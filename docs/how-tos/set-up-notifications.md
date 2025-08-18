---
title: Receive notifications
layout: default
nav_order: 4
parent: How Tos
---

# Receive notifications

## Send email notifications on failed backup runs

To send out email notifications on failed backup runs, provide SMTP credentials, a sender and a recipient:

```yml
services:
  backup:
    image: offen/docker-volume-backup:v2
    environment:
      # ... other configuration values go here
      NOTIFICATION_URLS=smtp://me:secret@smtp.example.com:587/?fromAddress=no-reply@example.com&toAddresses=you@example.com
```

Notification backends other than email are also supported.
Refer to the documentation of [shoutrrr][shoutrrr-docs] to find out about options and configuration.

[shoutrrr-docs]: https://containrrr.dev/shoutrrr/v0.8/services/overview/

{: .note }
If you also want notifications on successful executions, set `NOTIFICATION_LEVEL` to `info`.

## Customize notifications

The title and body of the notifications can be tailored to your needs using [Go templates](https://pkg.go.dev/text/template).
Template sources must be mounted inside the container in `/etc/dockervolumebackup/notifications.d/`: any file inside this directory will be parsed.

```yml
services:
  backup:
    image: offen/docker-volume-backup:v2
    volumes:
      - ./customized.template:/etc/dockervolumebackup/notifications.d/01.template
```

The files have to define [nested templates](https://pkg.go.dev/text/template#hdr-Nested_template_definitions) in order to override the original values. An example:

{% raw %}
```
{{ define "title_success" -}}
✅ Successfully ran backup {{ .Config.BackupStopDuringBackupLabel }}
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
{% endraw %}

Template names that can be overridden are:
  - `title_success` (the title used for a successful execution)
  - `body_success` (the body used for a successful execution)
  - `title_failure` (the title used for a failed execution)
  - `body_failure` (the body used for a failed execution)

## Notification templates reference

Configuration, data about the backup run and helper functions will be passed to these templates, this page documents them fully.

### Data

Here is a list of all data passed to the template:

* `Config`: this object holds the configuration that has been passed to the script. The field names are the name of the recognized environment variables converted in PascalCase. (e.g. `BACKUP_STOP_DURING_BACKUP_LABEL` becomes `BackupStopDuringBackupLabel`)
* `Error`: the error that made the backup fail. Only available in the `title_failure` and `body_failure` templates
* `Stats`: objects that holds stats regarding script execution. In case of an unsuccessful run, some information may not be available.
  * `StartTime`: time when the script started execution
  * `EndTime`: time when the backup has completed successfully (after pruning)
  * `TookTime`: amount of time it took for the backup to run. (equal to `EndTime - StartTime`)
  * `LockedTime`: amount of time it took for the backup to acquire the exclusive lock
  * `LogOutput`: full log of the application
  * `Containers`: object containing stats about the docker containers
    * `All`: total number of containers
    * `ToStop`: number of containers matched by the stop rule
    * `Stopped`: number of containers successfully stopped
    * `StopErrors`: number of containers that were unable to be stopped (equal to `ToStop - Stopped`)
  * `Services`: object containing stats about the docker services (only populated when Docker is running in Swarm mode)
    * `All`: total number of services
    * `ToScaleDown`: number of containers matched by the scale down rule
    * `ScaledDwon`: number of containers successfully scaled down
    * `ScaleDownErrors`: number of containers that were unable to be stopped (equal to `ToScaleDown - ScaledDowm`)
  * `BackupFile`: object containing information about the backup file
    * `Name`: name of the backup file (e.g. `backup-2022-02-11T01-00-00.tar.gz`)
    * `FullPath`: full path of the backup file (e.g. `/archive/backup-2022-02-11T01-00-00.tar.gz`)
    * `Size`: size in bytes of the backup file
  * `Storages`: object that holds stats about each storage
    * `Local`, `S3`, `WebDAV`, `Azure`, `Dropbox` or `SSH`:
      * `Total`: total number of backup files
      * `Pruned`: number of backup files that were deleted due to pruning rule
      * `PruneErrors`: number of backup files that were unable to be pruned

### Functions

Some formatting and helper functions are also available:

* `formatTime`: formats a time object using [RFC3339](https://datatracker.ietf.org/doc/html/rfc3339) format (e.g. `2022-02-11T01:00:00Z`)
* `formatBytesBin`: formats an amount of bytes using powers of 1024 (e.g. `7055258` bytes will be `6.7 MiB`) 
* `formatBytesDec`: formats an amount of bytes using powers of 1000 (e.g. `7055258` bytes will be `7.1 MB`)
* `env`: returns the value of the environment variable of the given key if set
* `toJson`: converting object to JSON
* `toPrettyJson`: converting object to pretty JSON

## Special characters in notification URLs

The value given to `NOTIFICATION_URLS` is a comma separated list of URLs.
If such a URL contains special characters (e.g. commas) these need to be URL encoded.
To obtain an encoded version of your URL, you can use the CLI tool provided by `shoutrrr` (which is the library used for sending notifications):

```
docker run --rm -ti containrrr/shoutrrr generate [service]
```

where service is any of the [supported services][shoutrrr-docs], e.g. for SMTP:

```
docker run --rm -ti containrrr/shoutrrr generate smtp
```
