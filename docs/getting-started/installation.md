---
title: Production installation
description: Install EdgeComet for production use
---

# Production installation

EdgeComet requires minimal dependencies: Redis for service discovery and cache indexing, and optionally Prometheus for monitoring. The project has been extensively tested on Ubuntu and is fully compatible.

Depending on your installation scheme (single machine, distributed renders, multiple gateways), your configuration will differ.

## Prerequisites

- Go 1.21+ (for building)
- Redis 6.0+
- Google Chrome (latest stable) - for Render Service machines
- Linux (Ubuntu LTS recommended)

## Install Google Chrome

On Render Service machines, install Google Chrome:

```bash
apt-get update && apt-get install -y fonts-liberation wget gnupg

wget -q -O - https://dl.google.com/linux/linux_signing_key.pub | \
  gpg --dearmor -o /usr/share/keyrings/google-chrome.gpg

echo "deb [arch=amd64 signed-by=/usr/share/keyrings/google-chrome.gpg] \
  http://dl.google.com/linux/chrome/deb/ stable main" | \
  tee /etc/apt/sources.list.d/google-chrome.list

apt-get update && apt-get install -y google-chrome-stable
```

## Build from source

```bash
# Clone repository
git clone <repository-url>
cd edgecomet

# Build all services
GOOS=linux GOARCH=amd64 go build -o bin/edge-gateway ./cmd/edge-gateway
GOOS=linux GOARCH=amd64 go build -o bin/render-service ./cmd/render-service
GOOS=linux GOARCH=amd64 go build -o bin/cache-daemon ./cmd/cache-daemon
```

## Directory structure

```
/opt/edgecomet/
  bin/
    edge-gateway
    render-service
    cache-daemon
  configs/
    edge-gateway.yaml
    render-service.yaml
    cache-daemon.yaml
    hosts.d/
      example.com.yaml
  cache/
    # rendered HTML files
  logs/
    # log files (if file logging enabled)
```

## Create directories

```bash
sudo mkdir -p /opt/edgecomet/{bin,configs/hosts.d,cache,logs}
```

## Create service user

```bash
sudo useradd -r -s /bin/false edgecomet
sudo chown -R edgecomet:edgecomet /opt/edgecomet
```

## Copy binaries

```bash
sudo cp bin/* /opt/edgecomet/bin/
sudo chmod +x /opt/edgecomet/bin/*
```

## Copy configurations

```bash
sudo cp configs/sample/*.yaml /opt/edgecomet/configs/
sudo cp configs/sample/hosts.d/*.yaml /opt/edgecomet/configs/hosts.d/
```

## Set permissions

```bash
# Binaries
sudo chmod 755 /opt/edgecomet/bin/*

# Configs (read-only for service)
sudo chmod 640 /opt/edgecomet/configs/*.yaml
sudo chmod 640 /opt/edgecomet/configs/hosts.d/*.yaml

# Cache directory (writable)
sudo chmod 755 /opt/edgecomet/cache
```

## Verify installation

```bash
/opt/edgecomet/bin/edge-gateway -h
/opt/edgecomet/bin/render-service -h
```

Use the `-t` flag to validate your Edge Gateway configuration without starting the service. This checks syntax, required fields, and host configuration files:

```bash
/opt/edgecomet/bin/edge-gateway -t -c /opt/edgecomet/configs/edge-gateway.yaml
```

You can also test how a specific URL would be processed by passing it as an argument:

```bash
/opt/edgecomet/bin/edge-gateway -t -c /opt/edgecomet/configs/edge-gateway.yaml \
  "https://example.com/products/widget?utm_source=google"
```

## Test manual startup

Before configuring systemd, verify the services start correctly. Open separate terminals for each service:

```bash
# Terminal 1: Start Edge Gateway
/opt/edgecomet/bin/edge-gateway -c /opt/edgecomet/configs/edge-gateway.yaml

# Terminal 2: Start Render Service
/opt/edgecomet/bin/render-service -c /opt/edgecomet/configs/render-service.yaml
```

You should see startup logs indicating successful initialization. Press Ctrl+C to stop each service.

