---
title: Cache issues
description: Diagnose and fix caching problems
---

# Cache issues

## Cache misses when expecting hits

### Symptoms
- `X-Render-Source: rendered` instead of `cache`
- Every request triggers new render

### Causes
- Cache key mismatch (URL normalization)
- Dimension not matching
- TTL expired
- Cache file missing

### Diagnosis
- Check `X-Render-Cache` header
- Compare cache keys in logs
- Verify dimension matching with `X-Unmatched-Dimension`

### Solutions
- Review URL normalization settings
- Check dimension `match_ua` patterns
- Verify TTL configuration

## Stale content served

### Symptoms
- Old content returned after site updates
- `X-Cache-Age` showing long duration

### Causes
- TTL too long
- Bot-triggered recache not working
- Cache daemon not running

### Solutions
- Reduce `render.cache.ttl`
- Verify cache daemon configuration
- Check recache trigger settings

## Cache key mismatches

### Symptoms
- Same page cached multiple times
- Dimension confusion

### Causes
- Query parameter handling differences
- Case sensitivity issues
- Trailing slash inconsistency

### Solutions
- Review `url_normalization` settings
- Check `query_params` whitelist/blacklist
- Enable lowercase normalization

## Storage permission errors

### Symptoms
- `permission denied` in logs
- Cache writes failing

### Causes
- Incorrect file ownership
- Directory not writable
- Disk full

### Solutions
- Check `storage.base_path` permissions
- Verify service user ownership
- Monitor disk space

## Redis connection issues

### Symptoms
- `redis connection refused` errors
- Intermittent cache failures

### Causes
- Redis not running
- Wrong connection settings
- Network/firewall blocking

### Solutions
- Verify Redis status: `redis-cli ping`
- Check `redis.addr` in config
- Review firewall rules
