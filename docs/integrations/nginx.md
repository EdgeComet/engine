---
title: nginx reverse proxy
description: Configure nginx to route crawler traffic through Edge Gateway for server-side rendering
---

# nginx reverse proxy

Configure nginx as a reverse proxy to route crawler traffic through Edge Gateway for server-side rendering of JavaScript-heavy pages.

## Prerequisites

- Running Edge Gateway instance (see [Quick Start](/quick-start))
- Configured host with `render_key` and `domain`
- nginx 1.14+ with `ngx_http_proxy_module`

## How it works

nginx sits between clients and your origin server. When a crawler requests a page, nginx routes the request to Edge Gateway for pre-rendered HTML. Regular users go directly to your origin server, preserving cookies, sessions, and authentication.

```mermaid
flowchart LR
    %% Palette Definitions
    classDef entry fill:#89B4FA,stroke:#6C7086,stroke-width:2px,color:#1E1E2E;
    classDef process fill:#313244,stroke:#6C7086,stroke-width:2px,color:#CDD6F4;
    classDef decision fill:#45475A,stroke:#6C7086,stroke-width:2px,color:#CDD6F4;

    Client([Client / Crawler])
    Nginx[Nginx Proxy]
    IsCrawler{Is Crawler?}
    EG[Edge Gateway<br>Rendered HTML]
    Origin[Origin Server<br>Dynamic HTML]

    Client --> Nginx
    Nginx --> IsCrawler

    IsCrawler -- Yes --> EG
    IsCrawler -- No --> Origin

    class Client entry;
    class Nginx,EG,Origin process;
    class IsCrawler decision;

    linkStyle default stroke:#6C7086,stroke-width:2px;
```

Edge Gateway endpoint: `GET /render?url=<target-url>`

Required header: `X-Render-Key` (from your host configuration)

## Map configuration

Maintain detection logic in a separate file and include it in your server block. This configuration catches crawlers using generic keywords plus explicit patterns for crawlers without these keywords in their name.

::: code-group

```nginx [nginx/conf.d/edge-gateway-map.conf]
# 1. Detect crawlers by User-Agent
map $http_user_agent $eg_crawler {
    default 0;

    # Generic crawler keywords
    "~*bot"                       1;
    "~*crawl"                     1;
    "~*spider"                    1;
    "~*slurp"                     1;

    # Crawlers without generic keywords in name
    "~*WhatsApp"                  1;
    "~*Snapchat"                  1;
    "~*facebookexternalhit"       1;
    "~*AMZN-User"                 1;
    "~*Claude-User"               1;
    "~*Perplexity-User"           1;
    "~*ChatGPT-User"              1;
}

# 2. Skip static assets (inherits $eg_crawler, disables for static files)
map $uri $eg_skip_render {
    default $eg_crawler;
    "~*\.(avif|css|eot|gif|gz|ico|jpeg|jpg|js|json|map|mp3|mp4|ogg|otf|pdf|png|svg|ttf|txt|wasm|wav|webm|webp|woff|woff2|xml|zip)$" 0;
}

# 3. Loop prevention (inherits $eg_skip_render, disables for renderer callbacks)
map $http_x_edge_render $eg_should_render {
    default $eg_skip_render;
    "~."    0;
}
```

:::

For alternative crawler detection approaches and detailed configuration explanations, see the [nginx reference](./nginx-reference).

## Server configuration

Use the `$eg_should_render` variable from the map configuration to route crawler traffic.

```nginx [nginx/sites-enabled/example.com.conf]
# Define upstreams for flexibility
upstream backend {
    server 127.0.0.1:3000;
}

upstream rendergw {
    server 127.0.0.1:10070;
}

server {
    listen 80;
    server_name example.com;

    # Include the detection logic
    include conf.d/edge-gateway-map.conf;

    location / {
        # Route crawlers to Edge Gateway (logic computed in maps above)
        error_page 418 = @edge_render;
        if ($eg_should_render = 1) {
            return 418;
        }

        # Regular traffic goes to origin
        proxy_pass http://backend;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location @edge_render {
        internal;

        proxy_pass http://rendergw/render?url=$scheme://$host$request_uri;

        proxy_set_header X-Render-Key "your_render_key_here";
        proxy_set_header User-Agent $http_user_agent;

        # Forward original client details
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Request-ID $request_id;

        # Timeouts should exceed EG render timeout
        proxy_connect_timeout 10s;
        proxy_read_timeout 60s;
        proxy_send_timeout 10s;
    }
}
```

Replace:
- `127.0.0.1:3000` with your origin server address.
- `127.0.0.1:10070` with your Edge Gateway address.
- `your_render_key_here` with your host's `render_key`.
- `example.com` with your domain.

## Verifying the setup

### Test crawler detection

Send a request with a crawler User-Agent:

```bash
curl -v \
  -H "User-Agent: Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)" \
  "http://example.com/"
```

Check response headers:
- `X-Render-Source: rendered` or `X-Render-Source: cache` confirms EG processed the request.

### Test regular user

Send a request with a browser User-Agent:

```bash
curl -v \
  -H "User-Agent: Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36" \
  "http://example.com/"
```

Request should go directly to origin with no `X-Render-*` headers in response.

### Debug crawler detection

Add a temporary header to see the final rendering decision:

```nginx
add_header X-EG-Should-Render $eg_should_render;
```

## Troubleshooting

### 403 Forbidden from Edge Gateway

- Verify `X-Render-Key` matches your host configuration.
- Check the domain in the URL matches your configured `domain`.
- Confirm the host is `enabled: true`.

### Timeout errors

- Increase `proxy_read_timeout` to exceed your EG render timeout.
- Check EG logs for render failures.
- Verify render service is running and registered.

### Crawlers not being detected

- Check the value of `$eg_should_render` using the debug header above.
- Add missing patterns to the `$eg_crawler` map block.
- Verify User-Agent header is being passed correctly.

### Infinite loops

- Ensure `proxy_pass` in the `@edge_render` block uses variables like `$scheme` and `$host` correctly.
- Verify that `X-Edge-Render` header is not being stripped by any other proxy in the chain.

### Cache not working

- Verify `X-Render-Source` header shows `cache` on repeat requests.
- Check `X-Cache-Age` header for cache duration.
- Review EG cache configuration and storage permissions.
- Ensure nginx itself is not agressively caching the render endpoint (unless configured to respect Vary headers).

## Related documentation

- [nginx reference](./nginx-reference) - Detailed configuration explanations
- [Diagnostic headers](/edge-gateway/x-headers) - Response header reference
- [Dimensions](/edge-gateway/dimensions) - Crawler detection via User-Agent matching
- [Caching](/edge-gateway/caching) - Cache configuration
- [Bypass mode](/edge-gateway/bypass-mode) - Direct origin fetching
