# Copyright 2021 - Offen Authors <hioffen@posteo.de>
# SPDX-License-Identifier: MPL-2.0

FROM golang:1.18-alpine as builder

WORKDIR /app
COPY . .
RUN go mod download
WORKDIR /app/cmd/backup
RUN go build -o backup .

FROM alpine:3.16

WORKDIR /root

RUN apk add --no-cache ca-certificates

COPY --from=builder /app/cmd/backup/backup /usr/bin/backup

COPY ./entrypoint.sh /root/
RUN chmod +x entrypoint.sh

ENTRYPOINT ["/root/entrypoint.sh"]
