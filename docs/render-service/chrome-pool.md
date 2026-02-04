---
title: Chrome pool
description: How Render Service manages Chrome instances for rendering
---

# Chrome pool

## Pool architecture

Instead of running one browser instance with many tabs, Render Service runs multiple instances with one tab each. Chrome is prone to errors, memory leaks, and sudden freezes. By separating the load through instances, you can easily observe any issues, kill and restart the problematic one. This achieves system stability and fast rendering speed.

Each Chrome instance consists of:
- One browser process
- One tab context created per render request
- Immediate tab cleanup after rendering completes

## Pool sizing

### Hardware considerations

The pool size depends on available hardware and the pages you render. For an average server with 8 cores and 16-32GB RAM, start with 10-15 instances. Do not exceed 20-25 instances per server. It's better to have two weaker physical servers rather than one powerful.

### Page complexity factor

Pay attention to how heavy the rendering is. Simple pages with moderate JavaScript can achieve a high render rate, while complex pages need a reduced pool size to keep the server's load average in a healthy range.

## Instance lifecycle

### Acquisition

When RS receives a render request, it acquires an available Chrome instance from the pool and creates a new tab context.

### Rendering

Chrome navigates to the target URL and starts loading the page. During loading, RS blocks configured resource types (images, fonts, media) and URL patterns (analytics, tracking scripts) to reduce bandwidth and speed up rendering.

Chrome executes JavaScript, constructs the DOM, and fetches allowed resources. RS waits for the specified lifecycle event: DOMContentLoaded, load, or networkIdle. After the event fires, RS applies the optional additional_wait delay for late-executing JavaScript.

### Release

Once waiting completes (or a timeout occurs), RS extracts the final HTML from the rendered page, captures the HTTP status code Chrome observed, and closes the tab context. The instance returns to the pool immediately.

## Automatic restarts

### Restart triggers

Chrome instances accumulate memory leaks and internal state over time, degrading performance. RS automatically restarts instances based on configured thresholds:
- After a set number of render requests
- After a runtime duration

### Restart process

Restarts happen between renders when the instance is idle, not during active rendering. The instance is removed from the pool, terminated, and a new one is created in its place. These policies maintain long-term stability without manual intervention or scheduled maintenance windows.

## Configuration example

::: code-group
```yaml [render-service.yaml]
chrome:
  pool_size: 12
  restart:
    after_count: 1000
    after_time: "1h"
```
:::
