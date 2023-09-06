---
title: Use with Docker Swarm
layout: default
parent: How Tos
nav_order: 13
---

# Use with Docker Swarm

By default, Docker Swarm will restart stopped containers automatically, even when manually stopped.
If you plan to have your containers / services stopped during backup, this means you need to apply the `on-failure` restart policy to your service's definitions.
A restart policy of `always` is not compatible with this tool.

---

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

