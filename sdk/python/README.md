# cdpfleet

Launch real remote browsers on [cdpfleet](https://cdpfleet.com) and drive them with your own
Playwright. No dependencies: bring your own `playwright` so you control the client version.

```sh
pip install cdpfleet playwright
export CDPFLEET_API_KEY=...
```

## Sync

```python
from playwright.sync_api import sync_playwright
from cdpfleet import Cdpfleet

fleet = Cdpfleet()  # api_key=..., or CDPFLEET_API_KEY
with sync_playwright() as p, fleet.launch("chrome", proxy="http://user:pass@host:port", headless=False) as session:
    browser = session.connect(p.chromium)
    page = browser.new_page()
    page.goto("https://example.com")
    print(page.title())
    print(session.live_link("view")["live_url"])
```

## Async

```python
import asyncio
from playwright.async_api import async_playwright
from cdpfleet import AsyncCdpfleet

async def main():
    fleet = AsyncCdpfleet()
    async with async_playwright() as p, fleet.launch("chromium", headless=True) as session:
        browser = await session.connect(p.chromium)
        page = await browser.new_page()
        await page.goto("https://example.com")
        print(await page.title())

asyncio.run(main())
```

## keep_alive

A session launched with `keep_alive=True` survives a disconnect: call `session.connect(...)`
again to re-attach to the same browser, pages included. `session.close()` (or leaving the
`with` block) closes your connections and ends a `keep_alive` session; `session.end()` ends it
without touching your connections.

Launch options are passed through as the JSON body (`proxy`, `headless`, `cdp`, `keep_alive`,
`record`, `proxy_rules`, ...); 503 and 429 are retried for up to `max_wait` seconds. Errors raise
`CdpfleetError` with `status`, `code` and `body`.

Docs: https://cdpfleet.com/docs/sdk
