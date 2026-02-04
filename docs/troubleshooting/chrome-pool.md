---
title: Chrome pool issues
description: Diagnose and fix Chrome rendering pool problems
---

# Chrome pool issues

## Chrome not starting

### Symptoms
- `failed to start chrome` in logs
- Render Service fails to start

### Causes
- Chrome not installed
- Wrong `chrome.path` in config
- Missing dependencies (Linux)
- Sandbox issues

### Solutions
- Verify Chrome installation: `google-chrome --version`
- Check `chrome.path` configuration
- Install missing libraries (see Linux dependencies)
- Try `--no-sandbox` flag (not recommended for production)

## Pool exhaustion

### Symptoms
- `no available chrome instances` errors
- Requests queuing or timing out
- High latency spikes

### Causes
- Pool size too small for traffic
- Renders taking too long
- Chrome instances not releasing

### Solutions
- Increase `chrome.pool_size`
- Reduce render timeouts
- Check for stuck renders in logs
- Monitor `chrome_pool_active` metric

## Memory issues

### Symptoms
- OOM kills in system logs
- Chrome processes consuming excessive RAM
- System becoming unresponsive

### Causes
- Pool size too large for available memory
- Memory leaks in long-running instances
- Heavy pages accumulating memory

### Solutions
- Reduce `chrome.pool_size`
- Lower `chrome.max_requests_per_instance`
- Enable `chrome.restart_on_memory_mb` threshold
- Add swap space (temporary)

## Instance restart problems

### Symptoms
- Chrome instances cycling frequently
- Restart loops in logs

### Causes
- `max_requests_per_instance` too low
- Chrome crashing on certain pages
- Resource limits too aggressive

### Solutions
- Increase `max_requests_per_instance`
- Check for problematic URLs causing crashes
- Review Chrome flags configuration

## Render Service registration failures

### Symptoms
- `failed to register in service registry`
- Edge Gateway reports no render services

### Causes
- Redis connectivity issues
- Duplicate service IDs
- Heartbeat failures

### Solutions
- Verify Redis connection from Render Service
- Ensure unique `server.id` per instance
- Check heartbeat interval and timeout settings
