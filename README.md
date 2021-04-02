# docker-volume-backup

Backup Docker volumes to any S3 compatible storage.

The `offen/docker-volume-backup` Docker image can be used as a sidecar container to an existing Docker setup. It handles recurring backups of Docker volumes to any S3 compatible storage and rotates away old backups if configured.
