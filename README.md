<a href="https://www.offen.software/">
    <img src="https://offen.github.io/press-kit/avatars/avatar-OS-header.svg" alt="offen.software logo" title="offen.software" width="60px"/>
</a>

# docker-volume-backup

Backup Docker volumes locally or to any S3, WebDAV, Azure Blob Storage, Dropbox or SSH compatible storage.

The [offen/docker-volume-backup](https://hub.docker.com/r/offen/docker-volume-backup) Docker image can be used as a lightweight (below 15MB) companion container to an existing Docker setup.
It handles __recurring or one-off backups of Docker volumes__ to a __local directory__, __any S3, WebDAV, Azure Blob Storage, Dropbox or SSH compatible storage (or any combination thereof) and rotates away old backups__ if configured. It also supports __encrypting your backups using GPG__ and __sending notifications for (failed) backup runs__.

Documentation is found at <https://offen.github.io/docker-volume-backup>
  - [Quickstart](https://offen.github.io/docker-volume-backup)
  - [Configuration Reference](https://offen.github.io/docker-volume-backup/reference/)
  - [How Tos](https://offen.github.io/docker-volume-backup/how-tos/)
  - [Recipes](https://offen.github.io/docker-volume-backup/recipes/)

---

## Quickstart

### Recurring backups in a compose setup

Add a `backup` service to your compose setup and mount the volumes you would like to see backed up:

```yml
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

Alternatively, pass a `--env-file` in order to use a full config as described [in the docs](https://offen.github.io/docker-volume-backup/reference/).

### Looking for help?

In case your are looking for help or guidance on how to incorporate docker-volume-backup into your existing setup, consider [becoming a sponsor](https://github.com/sponsors/offen?frequency=one-time) and book a one hour consulting session.

---

Copyright &copy; 2024 <a target="_blank" href="https://www.offen.software">offen.software</a> and contributors.
Distributed under the <a href="https://github.com/offen/docker-volume-backup/tree/main/LICENSE">MPL-2.0 License</a>.
