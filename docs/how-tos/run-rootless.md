---
title: Use the image as a non-root user
layout: default
parent: How Tos
nav_order: 16
---

# Use the image as a non-root user

{: .important }
Running as a non-root user limits interaction with the Docker Daemon.
If you want to stop and restart containers and services during backup, and the host's Docker daemon is running as root, you will also need to run this tool as root.

By default, this image executes backups using the `root` user.
In case you prefer to use a different user, you can use Docker's [`user` ](https://docs.docker.com/engine/reference/run/#user) option, passing the user and group id:

```console
docker run --rm \
  -v data:/backup/data \
  --env AWS_ACCESS_KEY_ID="<xxx>" \
  --env AWS_SECRET_ACCESS_KEY="<xxx>" \
  --env AWS_S3_BUCKET_NAME="<xxx>" \
  --entrypoint backup \
  --user 1000:1000 \
  offen/docker-volume-backup:v2
```

or in a compose file:

```yml
services:
  backup:
    image: offen/docker-volume-backup:v2
    user: 1000:1000
    # further configuration omitted ...
```
