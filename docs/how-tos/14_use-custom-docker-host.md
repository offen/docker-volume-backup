---
title: Use a custom Docker host
layout: default
parent: How Tos
nav_order: 14
---

# Use a custom Docker host

If you are interfacing with Docker via TCP, set `DOCKER_HOST` to the correct URL.
```ini
DOCKER_HOST=tcp://docker_socket_proxy:2375
```

In case you are using a socket proxy, it must support `GET` and `POST` requests to the `/containers` endpoint. If you are using Docker Swarm, it must also support the `/services` endpoint. If you are using pre/post backup commands, it must also support the `/exec` endpoint.

