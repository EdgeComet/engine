---
title: Systemd setup
description: Configure systemd services for EdgeComet.
---

# Systemd setup

This guide assumes you have installed the EdgeComet binaries in `/opt/edgecomet/bin` and configuration files in `/opt/edgecomet/configs`.

## Prerequisites

Before creating service files, set up the necessary user and directories.

### 1. Create service user

Create a dedicated system user for EdgeComet services.

```bash
# Create system user and group
sudo useradd -r -s /bin/false -d /opt/edgecomet edgecomet
```

### 2. Create directories and set permissions

Ensure the service user can write to log and cache directories.

```bash
# Create directories
sudo mkdir -p /var/log/edgecomet
sudo mkdir -p /var/cache/edgecomet

# Set ownership
sudo chown -R edgecomet:edgecomet /var/log/edgecomet
sudo chown -R edgecomet:edgecomet /var/cache/edgecomet
sudo chown -R edgecomet:edgecomet /opt/edgecomet
```

### 3. Install dependencies

For the **Render Service**, ensure a compatible browser is installed:

```bash
# Ubuntu/Debian
sudo apt-get update && sudo apt-get install -y chromium-browser
```

## Service files

### Edge Gateway

Create `/etc/systemd/system/edgecomet-edge-gateway.service`:

```ini
[Unit]
Description=EdgeComet Edge Gateway
After=network.target redis.service
Wants=redis.service

[Service]
Type=simple
User=edgecomet
Group=edgecomet
WorkingDirectory=/opt/edgecomet
ExecStart=/opt/edgecomet/bin/edge-gateway -c /opt/edgecomet/configs/edge-gateway.yaml
Restart=on-failure
RestartSec=5
# High file descriptor limit for handling concurrent connections
LimitNOFILE=65535
# Allow 45s for graceful shutdown (app timeout is 30s)
TimeoutStopSec=45

[Install]
WantedBy=multi-user.target
```

### Render Service

Create `/etc/systemd/system/edgecomet-render-service.service`:

```ini
[Unit]
Description=EdgeComet Render Service
After=network.target redis.service
Wants=redis.service

[Service]
Type=simple
User=edgecomet
Group=edgecomet
WorkingDirectory=/opt/edgecomet
ExecStart=/opt/edgecomet/bin/render-service -c /opt/edgecomet/configs/render-service.yaml
Restart=on-failure
RestartSec=5
LimitNOFILE=65535
# Memory limit for Chrome pool (adjust based on system RAM)
MemoryMax=8G
# Allow 45s for graceful shutdown (draining in-flight renders)
TimeoutStopSec=45

[Install]
WantedBy=multi-user.target
```

### Cache Daemon (optional)

Create `/etc/systemd/system/edgecomet-cache-daemon.service`:

```ini
[Unit]
Description=EdgeComet Cache Daemon
After=network.target redis.service edgecomet-edge-gateway.service

[Service]
Type=simple
User=edgecomet
Group=edgecomet
WorkingDirectory=/opt/edgecomet
ExecStart=/opt/edgecomet/bin/cache-daemon -c /opt/edgecomet/configs/cache-daemon.yaml
Restart=on-failure
RestartSec=10

[Install]
WantedBy=multi-user.target
```

## Service ordering

Start services in this specific order to ensure proper registration and discovery:

1.  **Redis**: Must be running first for coordination.
2.  **Render Service**: Registers itself in Redis upon startup.
3.  **Edge Gateway**: Starts and discovers available Render Services from Redis.
4.  **Cache Daemon**: Connects to Edge Gateway.

## Enable and start services

```bash
# Reload systemd configuration
sudo systemctl daemon-reload

# Enable services to start on boot
sudo systemctl enable edgecomet-render-service
sudo systemctl enable edgecomet-edge-gateway
sudo systemctl enable edgecomet-cache-daemon

# Start services (respecting the order)
sudo systemctl start edgecomet-render-service
sudo systemctl start edgecomet-edge-gateway
sudo systemctl start edgecomet-cache-daemon
```

## Managing services

```bash
# Check status
sudo systemctl status edgecomet-edge-gateway
sudo systemctl status edgecomet-render-service

# View logs (follow in real-time)
sudo journalctl -u edgecomet-edge-gateway -f
sudo journalctl -u edgecomet-render-service -f

# Restart specific service
sudo systemctl restart edgecomet-edge-gateway
```

## Resource limits

### File descriptors
- `LimitNOFILE=65535` is set by default to handle high concurrency in the Edge Gateway and Chrome processes.

### Memory limits
- **Render Service**: Set `MemoryMax` to prevent the Chrome pool from exhausting system RAM.
- **Rule of thumb**: Allocate 500MB-1GB per Chrome instance. For 10 instances, allow ~8-10GB.

### CPU limits (optional)
- Use `CPUQuota` to restrict CPU usage if services share a host.
- Example: `CPUQuota=400%` allows the service to use up to 4 CPU cores.

## Graceful shutdown

EdgeComet services handle `SIGTERM` to ensure zero-downtime deployments:

- **Edge Gateway**: Stops accepting new connections and waits up to 30s for existing requests to complete.
- **Render Service**: Deregisters from Redis, stops the heartbeat, and finishes in-flight renders before exiting.