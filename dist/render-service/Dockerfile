ARG GO_VERSION=1.24.2
ARG ALPINE_VERSION=3.20

FROM engine:v0.1.0 AS app
FROM golang:${GO_VERSION}-bookworm AS builder

COPY --from=app /engine /builder
WORKDIR /builder
RUN go mod tidy

RUN CGO_ENABLED=0 GOARCH=amd64 GOOS=linux go build -o /render-service ./cmd/render-service

FROM alpine:${ALPINE_VERSION}

RUN apk add --no-cache \
    chromium \
    nss \
    freetype \
    ttf-freefont \
    curl

ENV PUPPETEER_SKIP_CHROMIUM_DOWNLOAD=true \
    PUPPETEER_EXECUTABLE_PATH=/usr/bin/chromium-browser

COPY --from=builder /render-service /opt/edgecomet/bin/render-service
COPY entrypoint.sh /usr/local/bin/entrypoint.sh

RUN chmod +x /opt/edgecomet/bin/render-service
RUN chmod +x /usr/local/bin/entrypoint.sh

EXPOSE 10080-10089

ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]