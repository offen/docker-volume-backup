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

If you do this as you seek to restrict access to the Docker socket, this tool is potentially calling the following Docker APIs:

| API | When |
|-|-|
| `Info` | always |
| `ContainerExecCreate` | running commands from `exec-labels` |
| `ContainerExecAttach` | running commands from `exec-labels` |
| `ContainerExecInspect` | running commands from `exec-labels` |
| `ContainerList` | always |
  `ServiceList` | Docker engine is running in Swarm mode |
| `ServiceInspect` | Docker engine is running in Swarm mode |
| `ServiceUpdate` | Docker engine is running in Swarm mode and `stop-during-backup` is used |
| `ConatinerStop` | `stop-during-backup` labels are applied to containers |
| `ContainerStart` | `stop-during-backup` labels are applied to container |

---

In case you are using [`docker-socket-proxy`][proxy], this means following permissions are required:

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
