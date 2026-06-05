# syntax=docker/dockerfile:1

FROM golang:1.24-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/kpl-adapter ./cmd/kpl-adapter

FROM alpine:3.21

RUN addgroup -S app && adduser -S app -G app

WORKDIR /app
RUN mkdir -p /data && chown -R app:app /app /data

COPY --from=build /out/kpl-adapter /usr/local/bin/kpl-adapter

USER app

ENV KPL_LISTEN_ADDR=0.0.0.0:8787
ENV KPL_CACHE_PATH=/data/kpl-cache.sqlite

EXPOSE 8787

ENTRYPOINT ["kpl-adapter"]
