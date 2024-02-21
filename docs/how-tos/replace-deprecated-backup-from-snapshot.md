---
title: Replace deprecated BACKUP_FROM_SNAPSHOT usage
layout: default
parent: How Tos
nav_order: 17
---

# Replace deprecated `BACKUP_FROM_SNAPSHOT` usage

Starting with version 2.15.0, the `BACKUP_FROM_SNAPSHOT` feature has been deprecated.
If you need to prepare your sources before the backup is taken, use `archive-pre`, `archive-post` and an intermediate volume:

```yml
version: '3'

services:
  my_app:
    build: .
    volumes:
      - data:/var/my_app
      - backup:/tmp/backup
    labels:
      - docker-volume-backup.archive-pre=cp -r /var/my_app /tmp/backup/my-app
      - docker-volume-backup.archive-post=rm -rf /tmp/backup/my-app

  backup:
    image: offen/docker-volume-backup:v2
    environment:
      BACKUP_SOURCES: /tmp/backup
    volumes:
      - backup:/backup:ro
      - /var/run/docker.sock:/var/run/docker.sock:ro

volumes:
  data:
  backup:
```
