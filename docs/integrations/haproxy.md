---
title: HAProxy reverse proxy
description: Configure HAProxy to route crawler traffic through Edge Gateway for server-side rendering
---

# HAProxy reverse proxy

Configure HAProxy as a reverse proxy to route crawler traffic through Edge Gateway for server-side rendering of JavaScript-heavy pages.

## Prerequisites

- Running Edge Gateway instance (see [Quick start](/quick-start))
- Configured host with `render_key` and `domain`
- HAProxy 2.1+ with `http-request set-path` support

## How it works

HAProxy sits between clients and your origin server. When a crawler requests a page, HAProxy routes the request to Edge Gateway for pre-rendered HTML. Regular users go directly to your origin server, preserving cookies, sessions, and authentication.

```
Crawler request ──► HAProxy ──► Edge Gateway ──► rendered HTML
User request    ──► HAProxy ──► Origin Server ──► dynamic response
```

Edge Gateway endpoint: `GET /render?url=<target-url>`

Required header: `X-Render-Key` (from your host configuration)

## Crawler detection approaches

Both approaches route only crawler traffic to Edge Gateway. Regular users always go directly to origin.

| Approach | Description | Use when |
|----------|-------------|----------|
| Conservative | Explicit list of known crawlers | You want predictable behavior with no false positives |
| Broad | Keyword "bot" + explicit patterns | You want to catch more crawlers including unknown ones |

## Option A: Conservative crawler list

Explicit patterns for known search engines, AI crawlers, social media, and messengers based on [bot aliases](/edge-gateway/dimensions#available-aliases).

::: code-group
```haproxy [haproxy.cfg]
frontend http-in
    bind *:80
    mode http

    # Search engines ($SearchBots alias)
    acl is_crawler hdr_sub(user-agent) -i Googlebot
    acl is_crawler hdr_sub(user-agent) -i bingbot

    # AI crawlers ($AIBots alias)
    acl is_crawler hdr_sub(user-agent) -i ChatGPT-User
    acl is_crawler hdr_sub(user-agent) -i GPTBot
    acl is_crawler hdr_sub(user-agent) -i OAI-SearchBot
    acl is_crawler hdr_sub(user-agent) -i PerplexityBot
    acl is_crawler hdr_sub(user-agent) -i Perplexity-User
    acl is_crawler hdr_sub(user-agent) -i ClaudeBot
    acl is_crawler hdr_sub(user-agent) -i Claude-User
    acl is_crawler hdr_sub(user-agent) -i Claude-SearchBot
    acl is_crawler hdr_sub(user-agent) -i Amazonbot
    acl is_crawler hdr_sub(user-agent) -i AMZN-User

    # Google Ads bots
    acl is_crawler hdr_sub(user-agent) -i AdsBot-Google
    acl is_crawler hdr_sub(user-agent) -i AdsBot-Google-Mobile

    # Social media ($Socials alias)
    acl is_crawler hdr_sub(user-agent) -i facebookexternalhit
    acl is_crawler hdr_sub(user-agent) -i twitterbot
    acl is_crawler hdr_sub(user-agent) -i Pinterestbot
    acl is_crawler hdr_sub(user-agent) -i Applebot

    # Messengers ($Messengers alias)
    acl is_crawler hdr_sub(user-agent) -i WhatsApp
    acl is_crawler hdr_sub(user-agent) -i Telegrambot
    acl is_crawler hdr_sub(user-agent) -i ViberBot
    acl is_crawler hdr_sub(user-agent) -i Snapchat
    acl is_crawler hdr_sub(user-agent) -i Discordbot
    acl is_crawler hdr_sub(user-agent) -i Slackbot

    # Static files - skip rendering
    acl is_static path_end .avif .css .eot .gif .gz .ico .jpeg .jpg .js .json .map .mp3 .mp4 .ogg .otf .pdf .png .svg .ttf .txt .wasm .wav .webm .webp .woff .woff2 .xml .zip

    # Loop prevention
    acl from_renderer hdr(x-edge-render) -m found

    # Routing logic
    use_backend edge_gateway if is_crawler !is_static !from_renderer
    default_backend origin

backend edge_gateway
    mode http
    timeout server 60s
    server rendergw 127.0.0.1:10070
    http-request set-header X-Render-Key "your_render_key_here"
    http-request set-path "/render?url=%[ssl_fc,iif(https,http)]://%[req.hdr(host)]%[path]%[query]"

backend origin
    mode http
    server app 127.0.0.1:3000
```
:::

Replace:
- `127.0.0.1:3000` with your origin server address
- `127.0.0.1:10070` with your Edge Gateway address
- `your_render_key_here` with your host's `render_key`

## Option B: Broad crawler detection

Catches any User-Agent containing "bot" plus explicit patterns for crawlers without "bot" in their name.

::: code-group
```haproxy [haproxy.cfg]
frontend http-in
    bind *:80
    mode http

    # Match any User-Agent containing "bot"
    acl is_crawler hdr_sub(user-agent) -i bot

    # Crawlers without "bot" in name
    acl is_crawler hdr_sub(user-agent) -i WhatsApp
    acl is_crawler hdr_sub(user-agent) -i Snapchat
    acl is_crawler hdr_sub(user-agent) -i facebookexternalhit
    acl is_crawler hdr_sub(user-agent) -i AMZN-User
    acl is_crawler hdr_sub(user-agent) -i Claude-User
    acl is_crawler hdr_sub(user-agent) -i Perplexity-User
    acl is_crawler hdr_sub(user-agent) -i ChatGPT-User

    # Static files - skip rendering
    acl is_static path_end .avif .css .eot .gif .gz .ico .jpeg .jpg .js .json .map .mp3 .mp4 .ogg .otf .pdf .png .svg .ttf .txt .wasm .wav .webm .webp .woff .woff2 .xml .zip

    # Loop prevention
    acl from_renderer hdr(x-edge-render) -m found

    # Routing logic
    use_backend edge_gateway if is_crawler !is_static !from_renderer
    default_backend origin
```
:::

The backend configuration is identical to Option A.

## Loop prevention

When Edge Gateway renders a page, the Render Service fetches the target URL from your origin server. Without loop prevention, HAProxy would detect this request as a crawler and route it back to Edge Gateway, creating an infinite loop.

The Render Service adds an `X-Edge-Render` header to all outgoing requests. The `from_renderer` ACL detects this header and prevents re-routing.

```
Crawler → HAProxy (crawler detected) → Edge Gateway → Render Service
                                                           ↓
Origin ← HAProxy (X-Edge-Render detected, skip) ← Chrome
```

The ACL that enables this:

```haproxy
acl from_renderer hdr(x-edge-render) -m found
```

## Configuration reference

### Required headers

| Header | Description |
|--------|-------------|
| `X-Render-Key` | Authentication token from host configuration. |

### Recommended timeouts

```haproxy
defaults
    timeout connect 10s
    timeout client 30s
    timeout server 60s
```

Set `timeout server` higher than your Edge Gateway `render.timeout` configuration.

## Verifying the setup

### Test crawler detection

Send a request with a crawler User-Agent:

```bash
curl -v \
  -H "User-Agent: Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)" \
  "http://example.com/"
```

Check response headers:
- `X-Render-Source: rendered` or `X-Render-Source: cache` confirms Edge Gateway processed the request

### Test regular user

Send a request with a browser User-Agent:

```bash
curl -v \
  -H "User-Agent: Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36" \
  "http://example.com/"
```

The request should go directly to origin with no `X-Render-*` headers in response.

### Debug crawler detection

Add a temporary header to verify ACL matching:

```haproxy
http-response set-header X-Debug-Crawler %[var(txn.is_crawler)]
```

## Troubleshooting

### 403 Forbidden from Edge Gateway

1. Verify `X-Render-Key` matches your host configuration
2. Check the domain in the URL matches your configured `domain`
3. Confirm the host is `enabled: true`

### Timeout errors

1. Increase `timeout server` to exceed your Edge Gateway render timeout
2. Check Edge Gateway logs for render failures
3. Verify render service is running and registered

### Crawlers not being detected

1. Add the debug header shown above to check ACL evaluation
2. Verify ACL patterns match the User-Agent string
3. Confirm User-Agent header is being passed correctly

### Cache not working

1. Verify `X-Render-Source` header shows `cache` on repeat requests
2. Check `X-Cache-Age` header for cache duration
3. Review Edge Gateway cache configuration and storage permissions

## Related documentation

- [Diagnostic headers](/edge-gateway/x-headers)
- [Dimensions](/edge-gateway/dimensions)
- [Caching](/edge-gateway/caching)
- [Bypass mode](/edge-gateway/bypass-mode)
