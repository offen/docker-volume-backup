---
title: Encrypting backups
layout: default
parent: How Tos
nav_order: 7
---

# Encrypting backups

The image supports encrypting backups using one of two available methods: **GPG** or **[age](https://age-encryption.org/)**

## Using GPG encryption

In case a `GPG_PASSPHRASE` or `GPG_PUBLIC_KEY_RING` environment variable is set, the backup archive will be encrypted using the given key and saved as a `.gpg` file instead.

Assuming you have `gpg` installed, you can decrypt such a backup using (your OS will prompt for the passphrase before decryption can happen):

```console
gpg -o backup.tar.gz -d backup.tar.gz.gpg
```

## Using age encryption

age allows backups to be encrypted with either a symmetric key (password) or a public key. One of those options are available for use.

Given `AGE_PASSPHRASE` being provided, the backup archive will be encrypted with the passphrase and saved as a `.age` file instead. Refer to age documentation for how to properly decrypt.

Given `AGE_PUBLIC_KEYS` being provided (allowing multiple by separating each public key with `,`), the backup archive will be encrypted with the provided public keys. It will also result in the archive being saved as a `.age` file.
