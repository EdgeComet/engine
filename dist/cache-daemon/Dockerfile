ARG GO_VERSION=1.24.2
ARG ALPINE_VERSION=3.20

FROM engine:v0.1.0 AS app
FROM golang:${GO_VERSION}-bookworm AS builder

COPY --from=app /engine /builder
WORKDIR /builder
RUN go mod tidy

RUN CGO_ENABLED=0 GOARCH=amd64 GOOS=linux go build -o /cache-daemon ./cmd/cache-daemon

FROM alpine:${ALPINE_VERSION}

RUN apk add --no-cache curl

COPY --from=builder /cache-daemon /opt/edgecomet/bin/cache-daemon

COPY entrypoint.sh /usr/local/bin/entrypoint.sh

RUN chmod +x /opt/edgecomet/bin/cache-daemon
RUN chmod +x /usr/local/bin/entrypoint.sh

EXPOSE 10090-10099

ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]