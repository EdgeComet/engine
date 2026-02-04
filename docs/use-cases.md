# Use Cases

## When to Use EdgeComet

**JavaScript rendering for SPAs**: Single Page Applications (React, Vue, Angular) that rely on client-side rendering need server-side rendering for search engines. EdgeComet ensures bots see complete content with metadata, structured data, and dynamic elements.

**Crawl budget optimization**: Large sites with thousands of pages face origin server bottlenecks when search bots crawl their content. Bypass mode caches content without rendering, serving pages in under 15ms and reducing origin load by 90%+.

**AI assistant and search engine readiness**: Modern AI assistants (ChatGPT, Claude, Perplexity) need properly rendered HTML to understand your content. EdgeComet ensures AI crawlers (GPTBot, ClaudeBot, PerplexityBot) receive complete pages with structured data.

**Dynamic content sites**: Sites with JavaScript-dependent content that bots cannot parse natively benefit from server-side rendering to ensure proper indexing and search visibility.

**Device-specific rendering**: Sites with different mobile and desktop experiences need dimension-specific caching to serve optimized content for different bot types and maximize search visibility across platforms.

**High-traffic sites**: Production deployments with horizontal scaling requirements can use cache sharding for distributed cache and eliminate cache misses across Edge Gateway instances.

## When NOT to Use EdgeComet

**Minimal JavaScript sites**: Sites with simple JavaScript enhancements that do not affect content visibility do not benefit from rendering overhead.

**Simple content sites**: Blogs, documentation sites, or content-heavy sites without dynamic elements can serve content directly without rendering.

**Real-time applications**: Applications requiring real-time updates or user-specific content cannot use shared cache effectively and should implement alternative solutions.

