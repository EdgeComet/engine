# What is Render Service
Render Service (RS) is a daemon dedicated to rendering HTML pages by loading them in headless Chrome and executing their JavaScript. It manages Chrome instances, waits for pages to fully load, and captures the final rendered HTML with all dynamically generated content visible to search engine bots and AI crawlers.

RS does not store or retrieve cache. Its single responsibility is rendering: receive a URL from EG, open it in Chrome, wait for completion, and return the HTML. All caching, sharding, and request coordination happen in EG.

Rendering behavior (timeouts, lifecycle events, resource blocking, additional wait) is configured in EG and passed to RS with each request. See [Render mode](../edge-gateway/render-mode.md) for configuration details.



## Role in the system
EG discovers RS through shared Redis automatically, and it load balances render requests for them.
RS on its side handles the Chrome pool management by keeping Chrome instances alive,
checking their health, and periodically restarting them.
EG reserves an available tab via Redis before sending the render request, ensuring the request will be processed immediately. There is no internal queue, locks, or any other system inside Render Service.  
It ensures that the render request will be processed immediately.



## Render Flow

When RS receives a render request from EG, it acquires an available Chrome instance from the pool and creates a new tab context. Chrome navigates to the target URL and starts loading the page. During loading, RS blocks configured resource types (images, fonts, media) and URL patterns (analytics, tracking scripts) to reduce bandwidth and speed up rendering.

Chrome executes JavaScript, constructs the DOM, and fetches allowed resources. RS waits for the specified lifecycle event: DOMContentLoaded (DOM ready), load (all resources loaded), or networkIdle. After the event fires, RS applies the optional additional_wait delay for late-executing JavaScript.

Once waiting completes (or a timeout occurs), RS extracts the final HTML from the rendered page, captures the HTTP status code Chrome observed, and closes the tab context. The instance returns to the pool immediately. RS sends the HTML back to EG along with render metrics: duration, status code, whether a timeout occurred, and bytes transferred. EG then handles caching and response to the client.

If rendering fails (Chrome error, navigation failure, timeout), RS returns an error response. EG falls back to stale cache or bypass mode based on configuration.


## Chrome pool

RS manages a pool of Chrome instances for rendering. Instead of running one browser with many tabs, it runs multiple instances with one tab each. This design provides isolation, easier error handling, and the ability to restart problematic instances without affecting others.

The pool automatically restarts instances based on request count or runtime duration to prevent memory leaks from degrading performance. For details on pool sizing, instance lifecycle, and configuration, see [Chrome pool](chrome-pool.md).


## Deployment
Render Service mostly uses CPU and does not require much disk I/O. It can be combined normally with EG on the same physical machine.
Instead of having one EG and multiple RS, we recommend pairing them on each server.
EG will work with the replication, which helps to improve the system stability and consistency.
By technical requirements, RS needs quite a powerful machine, and, usually, such type of hardware comes with enough SSDs that will be utilized for cache. It also leads to cost savings.



## Issues with Chrome
Chrome is a complex application that, despite its maturity, still has memory leaks and occasional freezes.
In rare cases, it may freeze completely and not even respond to restart attempts.
Usually, it happens when the system load is excessive and the server can't handle it.
You can freely kill the render service by using "kill -9 PID" and kill all frozen Chrome processes with "killall -9 chrome".
RS will be removed from the registry within a 3-second interval, and all rendering requests will be rerouted to other instances.
EG will handle this normally by using stale cache or serving content through bypass mode.
