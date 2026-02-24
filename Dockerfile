# syntax=docker/dockerfile:1

FROM golang:1.24-bookworm AS builder-base
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

FROM builder-base AS builder-render-service
RUN go build -o /out/render-service ./cmd/render-service

FROM builder-base AS builder-cache-daemon
RUN go build -o /out/cache-daemon ./cmd/cache-daemon

FROM builder-base AS builder-edge-gateway
RUN if [ -d ./cmd/edge-gateway ]; then \
            go build -o /out/edge-gateway ./cmd/edge-gateway; \
        elif [ -f ./bin/edge-gateway ]; then \
            cp ./bin/edge-gateway /out/edge-gateway; \
            chmod +x /out/edge-gateway; \
        else \
            echo "edge-gateway binary not found."; \
            echo "Ask client to provide cmd/edge-gateway source"; \
            echo "or Linux bin/edge-gateway binary."; \
            exit 1; \
        fi

FROM debian:bookworm-slim AS runtime-base

RUN apt-get update && apt-get install -y --no-install-recommends \
        ca-certificates \
        chromium \
        curl \
        tini \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --system --create-home --home-dir /home/edgecomet --shell /usr/sbin/nologin edgecomet

WORKDIR /app
ENV CHROME_BIN=/usr/bin/chromium
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