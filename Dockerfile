# Copyright 2021 - Offen Authors <hioffen@posteo.de>
# SPDX-License-Identifier: MPL-2.0

FROM golang:1.16-alpine as builder
ARG MC_VERSION=RELEASE.2021-06-13T17-48-22Z
RUN go install -ldflags "-X github.com/minio/mc/cmd.ReleaseTag=$MC_VERSION" github.com/minio/mc@$MC_VERSION

FROM alpine:3.14

WORKDIR /root

RUN apk add --update ca-certificates docker openrc gnupg
RUN update-ca-certificates
RUN rc-update add docker boot

COPY --from=builder /go/bin/mc /usr/bin/mc
RUN mc --version

COPY src/backup.sh src/entrypoint.sh /root/
RUN chmod +x backup.sh && mv backup.sh /usr/bin/backup \
  && chmod +x entrypoint.sh

ENTRYPOINT ["/root/entrypoint.sh"]
