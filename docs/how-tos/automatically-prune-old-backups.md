---
title: Automatically prune old backups
layout: default
parent: How Tos
nav_order: 3
---

# Automatically prune old backups

When `BACKUP_RETENTION_DAYS` is configured, the command will check if there are any archives in the remote storage backend(s) or local archive that are older than the given retention value and rotate these backups away.

{: .note }
Be aware that this mechanism looks at __all files in the target bucket or archive__, which means that other files that are older than the given deadline are deleted as well.
In case you need to use a target that cannot be used exclusively for your backups, you can configure `BACKUP_PRUNING_PREFIX` to limit which files are considered eligible for deletion:

```yml
services:
  # ... define other services using the `data` volume here
  backup:
    image: offen/docker-volume-backup:v2
    environment:
      BACKUP_FILENAME: backup-%Y-%m-%dT%H-%M-%S.tar.gz
      BACKUP_PRUNING_PREFIX: backup-
      BACKUP_RETENTION_DAYS: '7'
    volumes:
      - ${HOME}/backups:/archive
      - data:/backup/my-app-backup:ro
      - /var/run/docker.sock:/var/run/docker.sock:ro

volumes:
  data:
```
