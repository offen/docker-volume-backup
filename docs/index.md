---
title: Home
layout: home
nav_order: 1
---

# offen/docker-volume-backup
{:.no_toc}

Backup Docker volumes locally or to any S3, WebDAV, Azure Blob Storage, Dropbox or SSH compatible storage.
{: .fs-6 .fw-300 }

---

The [offen/docker-volume-backup](https://hub.docker.com/r/offen/docker-volume-backup) Docker image can be used as a lightweight (below 15MB) companion container to an existing Docker setup.
It handles __recurring or one-off backups of Docker volumes__ to a __local directory__, __any S3, WebDAV, Azure Blob Storage, Dropbox or SSH compatible storage (or any combination thereof) and rotates away old backups__ if configured. It also supports __encrypting your backups using GPG__ and __sending notifications for (failed) backup runs__.

{: .note }
Code and documentation for `v1` versions are found on [this branch][v1-branch].

[v1-branch]: https://github.com/offen/docker-volume-backup/tree/v1

---

1. TOC
{:toc}

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

### Available image registries

This Docker image is published to both Docker Hub and the GitHub container registry.
Depending on your preferences and needs, you can reference both `offen/docker-volume-backup` as well as `ghcr.io/offen/docker-volume-backup`:

```
docker pull offen/docker-volume-backup:v2
docker pull ghcr.io/offen/docker-volume-backup:v2
```

Documentation references Docker Hub, but all examples will work using ghcr.io just as well.

## Differences to `jareware/docker-volume-backup`

This image is heavily inspired by `jareware/docker-volume-backup`. We decided to publish this image as a simpler and more lightweight alternative because of the following requirements:

- The original image is based on `ubuntu` and requires additional tools, making it heavy.
This version is roughly 1/25 in compressed size (it's ~15MB).
- The original image uses a shell script, when this version is written in Go.
- The original image proposed to handle backup rotation through AWS S3 lifecycle policies.
This image adds the option to rotate away old backups through the same command so this functionality can also be offered for non-AWS storage backends like MinIO.
Local copies of backups can also be pruned once they reach a certain age.
- InfluxDB specific functionality from the original image was removed.
- `arm64` and `arm/v7` architectures are supported.
- Docker in Swarm mode is supported.
- Notifications on finished backups are supported.
- IAM authentication through instance profiles is supported.
