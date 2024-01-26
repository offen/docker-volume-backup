---
title: Use with Docker Swarm
layout: default
parent: How Tos
nav_order: 13
---

# Use with Docker Swarm

{: .note }
The mechanisms described in this page __do only apply when Docker is running in [Swarm mode][swarm]__.

[swarm]: https://docs.docker.com/engine/swarm/

## Stopping containers during backup

Stopping and restarting containers during backup creation when running Docker in Swarm mode is supported in two ways.

### Scaling services down to zero before scaling back up

When labeling a service in the `deploy` section, the following strategy for stopping and restarting will be used:

- The service is scaled down to zero replicas
- The backup is created
- The service is scaled back up to the previous number of replicas

{: .note }
This approach will only work for services that are deployed in __replicated mode__.

Such a service definition could look like:

```yml
services:
  app:
    image: myorg/myimage:latest
    deploy:
      labels:
        - docker-volume-backup.stop-during-backup=true
      replicas: 2
```

### Stopping the containers

This approach bypasses the services and stops containers directly, creates the backup and restarts the containers again.
As Docker Swarm would usually try to instantly restart containers that are manually stopped, this approach only works when using the `on-failure` restart policy.
A restart policy of `always` is not compatible with this approach.

Such a service definition could look like:

```yml
services:
  app:
    image: myapp/myimage:latest
    labels:
      - docker-volume-backup.stop-during-backup=true
    deploy:
      replicas: 2
      restart_policy:
        condition: on-failure
```

---

## Memory limit considerations

When running in Swarm mode, it's also advised to set a hard memory limit on your service (~25MB should be enough in most cases, but if you backup large files above half a gigabyte or similar, you might have to raise this in case the backup exits with `Killed`):

```yml
services:
  backup:
    image: offen/docker-volume-backup:v2
    deployment:
      resources:
        limits:
          memory: 25M
```

