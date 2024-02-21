# Copyright 2021 - Offen Authors <hioffen@posteo.de>
# SPDX-License-Identifier: MPL-2.0

FROM golang:1.22-alpine as builder

WORKDIR /app
COPY . .
RUN go mod download
WORKDIR /app/cmd/backup
RUN go build -o backup .

FROM alpine:3.19

WORKDIR /root

RUN apk add --no-cache ca-certificates && \
  chmod a+rw /var/lock

COPY --from=builder /app/cmd/backup/backup /usr/bin/backup

ENTRYPOINT ["/usr/bin/backup", "-foreground"]
