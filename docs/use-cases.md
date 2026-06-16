---
title: Use Cases
description: When to use the open-source EdgeComet engine - caching and crawl-budget optimization for any site, plus JavaScript rendering for bots that need it.
---

# Use Cases

## When to Use EdgeComet

**Fast bot responses for any site**: Cache is the core. Every bot request flows through EdgeComet, which serves prepared HTML from the filesystem in under 15ms regardless of how the origin performs. This applies to any stack - JavaScript SPAs, traditional CMS, or static sites.

**Crawl budget optimization**: Large sites with thousands of pages face origin bottlenecks when search and AI bots crawl their content. Caching serves pages in under 15ms and reduces origin load by 90%+, so bots crawl more of your pages in the same window. Bypass mode can cache content without rendering for paths that do not need it.

**Keeping cache fresh automatically**: Bot-triggered pre-caching (the Cache Daemon) recaches frequently crawled pages on idle capacity, keeping popular URLs up to date without re-rendering the whole site or harming real-time performance.

**AI assistant and search engine readiness**: Modern AI assistants (ChatGPT, Claude, Perplexity) need properly rendered HTML to understand your content. EdgeComet ensures AI crawlers (GPTBot, ClaudeBot, PerplexityBot) receive complete pages with structured data.

**JavaScript rendering for SPAs and dynamic sites**: Single Page Applications (React, Vue, Angular) and other JavaScript-dependent sites rely on client-side rendering that many bots cannot execute. EdgeComet pre-renders these pages so bots see complete content with metadata, structured data, and dynamic elements.

**Device-specific rendering**: Sites with different mobile and desktop experiences use dimension-specific caching to serve optimized content for different bot types.

**High-traffic sites**: Production deployments with horizontal scaling requirements use cache sharding to distribute cache and eliminate cache misses across Edge Gateway instances.

## When NOT to Use EdgeComet

**Real-time or user-specific content**: Applications that require real-time updates or per-user content cannot use a shared bot cache effectively and should implement alternative solutions.

**Sites that need neither caching nor rendering**: If your origin already serves bots fast static HTML and crawl budget is not a concern, the cache layer adds little. Rendering in particular is optional - pages with no JavaScript-dependent content do not need it, though caching may still help with crawl budget on large sites.

