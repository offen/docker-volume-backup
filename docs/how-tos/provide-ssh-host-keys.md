---
title: Provide public host key or known hosts when using the SSH backend
layout: default
parent: How Tos
nav_order: 21
---

# Provide public host key or known hosts when using the SSH backend

Up to version 2.49.1 the image would not check the SSH host's identity, which is unsafe.
This potentially leaks credentials or backup content to external attackers.

Starting with version 2.49.2, this is prevented, requiring you to either:

- provide the host's public key by setting either `SSH_HOST_PUBLIC_KEY` (the key's content) or `SSH_HOST_PUBLIC_KEY_FILE` (the fs location of the key in a file)
- provide the location of a valid known_hosts file by setting `SSH_KNOWN_HOSTS_FILE`
- dangerously set `SSH_INSECURE_IGNORE_HOST_KEY` to ignore host key checks

{: .warning }
Only set `SSH_INSECURE_IGNORE_HOST_KEY` as a last resort or in entirely airgapped environments.
