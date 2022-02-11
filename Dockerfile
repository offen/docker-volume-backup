# Copyright 2021 - Offen Authors <hioffen@posteo.de>
# SPDX-License-Identifier: MPL-2.0

FROM golang:1.17-alpine as builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/backup ./cmd/backup/
RUN go build -o backup cmd/backup/main.go

FROM alpine:3.15

WORKDIR /root

RUN apk add --update ca-certificates

COPY --from=builder /app/backup /usr/bin/backup

COPY ./entrypoint.sh /root/
RUN chmod +x entrypoint.sh

ENTRYPOINT ["/root/entrypoint.sh"]
