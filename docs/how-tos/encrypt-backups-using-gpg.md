---
title: Encrypt backups using GPG
layout: default
parent: How Tos
nav_order: 7
---

# Encrypt backups using GPG

The image supports encrypting backups using GPG out of the box.
In case a `GPG_PASSPHRASE` or `GPG_PUBLIC_KEYS` environment variable is set, the backup archive will be encrypted using the given key and saved as a `.gpg` file instead.

Assuming you have `gpg` installed, you can decrypt such a backup using (your OS will prompt for the passphrase before decryption can happen):

```console
gpg -o backup.tar.gz -d backup.tar.gz.gpg
```
