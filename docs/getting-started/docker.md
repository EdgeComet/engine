---
title: Docker setup
description: Run EdgeComet with Docker Compose
---

You can run EdgeComet with Docker Compose to start Redis, Render Service, and Edge Gateway with one command. The Cache Daemon service is included as an optional block in the compose file.

The Dockerfile uses the official `golang:1.24` image for builds and a Debian Bookworm runtime. Google Chrome is installed in the Render Service runtime image.

## Prerequisites

Before you start, make sure you have:

- Docker Engine 24+
- Docker Compose v2
- At least 4 GB RAM available for containers (Render Service uses headless Chrome)

## Quick start

From the project root, run:

```bash
docker compose up --build -d
```

This starts:

- `redis` on `6379`
- `render-service` on `10080`
- `edge-gateway` on `10070`

The startup order mirrors the systemd setup:

1. Redis
2. Render Service
3. Edge Gateway
4. Cache Daemon (optional)

## Verify the stack

Check container status:

```bash
docker compose ps
```

Check health endpoints:

```bash
curl http://localhost:10080/health
curl http://localhost:10070/health
```

If both services are healthy, you can test a render request:

```bash
curl -H "X-Render-Key: your-render-key" \
  -H "User-Agent: Mozilla/5.0 (compatible; Googlebot/2.1)" \
  "http://localhost:10070/render?url=https://example.com"
```

## Docker configuration

Docker-specific configs are in `configs/docker/`:

- `edge-gateway.yaml`
- `render-service.yaml`
- `cache-daemon.yaml`
- `hosts.d/`

These files are based on sample configs and already use `redis:6379` for container networking.

Important: in `configs/docker/render-service.yaml`, set `server.listen` to `render-service:10080` so Redis service discovery advertises a Docker-reachable address.
When using this setting, keep the render-service healthcheck target aligned (for example `http://render-service:10080/health`) rather than `localhost`.

## Enable Cache Daemon (optional)

The `cache-daemon` service is commented out in `docker-compose.yml` by default.

To enable it:

1. Uncomment the `cache-daemon` service block in `docker-compose.yml`
2. Make sure `internal.auth_key` in `configs/docker/edge-gateway.yaml` matches your intended internal auth key
3. Restart the stack

```bash
docker compose up --build -d
```

You can then query daemon status:

```bash
curl -H "X-Internal-Auth: change-me-to-a-secure-key-32chars" \
  http://localhost:10090/status
```

## Common commands

```bash
# View logs
docker compose logs -f

# Restart a single service
docker compose restart edge-gateway

# Stop and remove containers
docker compose down
```
