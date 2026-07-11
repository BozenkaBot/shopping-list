# syntax=docker/dockerfile:1

FROM golang:1.24-alpine AS build
WORKDIR /src

COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
COPY web ./web

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/lista-zakupow ./cmd/server

FROM alpine:3.21
RUN addgroup -S app && adduser -S -G app app
WORKDIR /app

COPY --from=build /out/lista-zakupow /app/lista-zakupow
COPY web/static /app/web/static

RUN mkdir -p /data && chown -R app:app /data /app
USER app

ENV ADDR=:8080
ENV DATA_FILE=/data/shopping-list.json

EXPOSE 8080
VOLUME ["/data"]

CMD ["/app/lista-zakupow"]
