---
title: Handle file uploads using third party tools
layout: default
parent: How Tos
nav_order: 10
---

# Handle file uploads using third party tools

If you want to use an unsupported storage backend, or want to use a third party (e.g. rsync, rclone) tool for file uploads, you can build a Docker image containing the required binaries off this one, and call through to these in lifecycle hooks.

For example, if you wanted to use `rsync`, define your Docker image like this:

```Dockerfile
FROM offen/docker-volume-backup:v2

RUN apk add rsync
```

Using this image, you can now omit configuring any of the supported storage backends, and instead define your own mechanism in a `docker-volume-backup.copy-post` label:

```yml
version: '3'

services:
  backup:
    image: your-custom-image
    restart: always
    environment:
      BACKUP_FILENAME: "daily-backup-%Y-%m-%dT%H-%M-%S.tar.gz"
      BACKUP_CRON_EXPRESSION: "0 2 * * *"
    labels:
      - docker-volume-backup.copy-post=/bin/sh -c 'rsync $$COMMAND_RUNTIME_ARCHIVE_FILEPATH /destination'
    volumes:
      - app_data:/backup/app_data:ro
      - /var/run/docker.sock:/var/run/docker.sock

  # other services defined here ...
volumes:
  app_data:
```

{: .note }
Commands will be invoked with the filepath of the tar archive passed as `COMMAND_RUNTIME_BACKUP_FILEPATH`.
