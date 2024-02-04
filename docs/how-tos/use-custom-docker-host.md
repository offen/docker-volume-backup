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

In case you are using [`docker-socket-proxy`][proxy], the following permissions are required:

| Permission | When |
|-|-|
| INFO | always required |
| CONTAINERS | always required |
| POST | required when using `stop-during-backup` labels |
| EXEC | required when using `exec`-labeled commands |
| SERVICES | required when running in Swarm mode |
| NODES | required when using `stop-during-backup` and running in Swarm mode |
| TASKS | required when using `stop-during-backup` and running in Swarm mode |
| ALLOW_START | required when labeling containers `stop-during-backup` |
| ALLOW_STOP | required when labeling containers `stop-during-backup` |


[proxy]: https://github.com/Tecnativa/docker-socket-proxy
