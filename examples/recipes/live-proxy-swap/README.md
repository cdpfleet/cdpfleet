# Swap the proxy mid-session without restarting the browser

Launch as usual (every session can be swapped; `"proxy_updatable"` is no longer needed), then `POST /admin/session/proxy` with the session id and a new `proxy` (or `proxy_rules`) on the host from your `wsUrl`: see [Swapping the proxy live](https://cdpfleet.com/docs/proxies#live-swap) for the call. Measured on prod with this launch: the first exit was in the US, the swap answered in 47 ms, and the next new connection left from Germany through `cdpfleet-resi-country-de-…`. The browser, its cookies and its pages stay; connections already open finish on the old proxy, so open a new page (or reload) to move ([case 06](https://cdpfleet.com/docs/cases/live-proxy-swap) has the details).

- [Node.js](node/example.mjs)
- [Python](python/example.py)
- [Java](java/Example.java)
- [C#](csharp/Program.cs)
- [Go](go/main.go)

Set `CDPFLEET_API_KEY` and replace the placeholder proxy with yours. Install the client named at the top of each file.
