# Notification templates reference

In order to customize title and body of notifications you'll have to write a [go template](https://pkg.go.dev/text/template) and mount it inside the `/etc/dockervolumebackup/notifications.d/` directory.

Configuration, data about the backup run and helper functions will be passed to this template, this page documents them fully.

## Data
Here is a list of all data passed to the template:

* `Config`: this object holds the configuration that has been passed to the script. The field names are the name of the recognized environment variables converted in PascalCase. (e.g. `BACKUP_STOP_CONTAINER_LABEL` becomes `BackupStopContainerLabel`)
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
  * `BackupFile`: object containing information about the backup file
    * `Name`: name of the backup file (e.g. `backup-2022-02-11T01-00-00.tar.gz`)
    * `FullPath`: full path of the backup file (e.g. `/archive/backup-2022-02-11T01-00-00.tar.gz`)
    * `Size`: size in bytes of the backup file
  * `Storages`: object that holds stats about each storage
    * `Local`, `S3`, `WebDAV` or `SSH`:
      * `Total`: total number of backup files
      * `Pruned`: number of backup files that were deleted due to pruning rule
      * `PruneErrors`: number of backup files that were unable to be pruned

## Functions

Some formatting functions are also available:

* `formatTime`: formats a time object using [RFC3339](https://datatracker.ietf.org/doc/html/rfc3339) format (e.g. `2022-02-11T01:00:00Z`)
* `formatBytesBin`: formats an amount of bytes using powers of 1024 (e.g. `7055258` bytes will be `6.7 MiB`) 
* `formatBytesDec`: formats an amount of bytes using powers of 1000 (e.g. `7055258` bytes will be `7.1 MB`)
