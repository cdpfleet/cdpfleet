# pip install playwright==1.60.0 requests
# env: CDPFLEET_API_KEY, PROXY_URL
# Playwright's sync API isn't thread-safe, so the parallel loads use the async API.
import asyncio
import json
import os
import time

import requests
from playwright.async_api import async_playwright

KEY = os.environ["CDPFLEET_API_KEY"]
URLS = [
    "https://books.toscrape.com/",
    "https://quotes.toscrape.com/",
    "https://example.com",
    "https://en.wikipedia.org/wiki/Web_scraping",
    "https://news.ycombinator.com/",
]


async def main():
    res = requests.post("https://starter.cdpfleet.com/chromium/session",
        headers={"x-api-key": KEY},
        json={"proxy": os.environ["PROXY_URL"], "headless": "new"}, timeout=60)
    if not res.ok:
        raise SystemExit(f"launch {res.status_code} {res.text}")

    async with async_playwright() as p:
        browser = await p.chromium.connect(res.json()["wsUrl"], headers={"x-api-key": KEY})
        try:
            async def extract(url):
                # One retry, in a fresh tab: over 30 loads through a rotating proxy, one occasionally fails.
                for attempt in (1, 2):
                    page = await browser.new_page()
                    try:
                        await page.goto(url, wait_until="domcontentloaded", timeout=60000)
                        return {"url": url, "title": await page.title()}
                    except Exception:
                        if attempt == 2:
                            raise
                    finally:
                        await page.close()

            # Three rounds of each, alternating; the best time of each method counts. One page that
            # stalls for 20 s in the proxy would otherwise decide a single comparison on its own.
            rounds = []
            for _ in range(3):
                t1 = time.time()
                sequential = [{**(await extract(url)), "method": "sequential"} for url in URLS]
                seq = round(time.time() - t1, 2)

                t2 = time.time()
                parallel = [{**r, "method": "parallel"} for r in await asyncio.gather(*(extract(url) for url in URLS))]
                par = round(time.time() - t2, 2)
                rounds.append({"sequential_seconds": seq, "parallel_seconds": par})
            seq_seconds = min(r["sequential_seconds"] for r in rounds)
            par_seconds = min(r["parallel_seconds"] for r in rounds)

            print(json.dumps({
                "sequential_seconds": seq_seconds,
                "parallel_seconds": par_seconds,
                "speedup": f"{round(seq_seconds / par_seconds, 1)}x",
                "rounds": rounds,
                "results": sequential + parallel,
            }, indent=2, ensure_ascii=False))
        finally:
            await browser.close()


asyncio.run(main())
