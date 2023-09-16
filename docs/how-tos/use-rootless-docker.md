---
title: Use with rootless Docker
layout: default
parent: How Tos
nav_order: 15
---

# Use with rootless Docker

It's also possible to use this image with a [rootless Docker installation][rootless-docker].
Instead of mounting `/var/run/docker.sock`, mount the user-specific socket into the container:

```yml
services:
  backup:
    image: offen/docker-volume-backup:v2
    # ... configuration omitted
    volumes:
      - backup:/backup:ro
      - /run/user/1000/docker.sock:/var/run/docker.sock:ro
```

[rootless-docker]: https://docs.docker.com/engine/security/rootless/
