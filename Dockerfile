# syntax=docker/dockerfile:1

FROM golang:1.24 AS builder-base
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

FROM builder-base AS builder-render-service
RUN go build -o /out/render-service ./cmd/render-service

FROM builder-base AS builder-cache-daemon
RUN go build -o /out/cache-daemon ./cmd/cache-daemon

FROM builder-base AS builder-edge-gateway
RUN go build -o /out/edge-gateway ./cmd/edge-gateway

FROM debian:bookworm-slim AS runtime-base

RUN apt-get update && apt-get install -y --no-install-recommends \
        ca-certificates \
        curl \
        fonts-liberation \
        gnupg \
        tini \
        wget \
    && wget -q -O - https://dl.google.com/linux/linux_signing_key.pub | apt-key add - \
    && sh -c 'echo "deb [arch=amd64] http://dl.google.com/linux/chrome/deb/ stable main" >> /etc/apt/sources.list.d/google.list' \
    && apt-get update && apt-get install -y --no-install-recommends \
        google-chrome-stable \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --system --create-home --home-dir /home/edgecomet --shell /usr/sbin/nologin edgecomet

WORKDIR /app
ENV CHROME_BIN=/usr/bin/google-chrome
ENTRYPOINT ["/usr/bin/tini", "--"]

FROM runtime-base AS render-service-runtime
COPY --from=builder-render-service /out/render-service /usr/local/bin/render-service
EXPOSE 10080 10089

FROM runtime-base AS edge-gateway-runtime
COPY --from=builder-edge-gateway /out/edge-gateway /usr/local/bin/edge-gateway
EXPOSE 10070 10071 10079

FROM runtime-base AS cache-daemon-runtime
COPY --from=builder-cache-daemon /out/cache-daemon /usr/local/bin/cache-daemon
EXPOSE 10090 10099