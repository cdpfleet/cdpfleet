# pip install playwright==1.60.0 aiohttp
# env: CDPFLEET_API_KEY, PROXY_URL
import asyncio
import json
import os
import time

import aiohttp
from playwright.async_api import async_playwright

KEY = os.environ["CDPFLEET_API_KEY"]
PAGES = 24


async def launch(http, stats):
    """Launch with the retries the API asks for: 429 (thread limit, launch rate) and 503
    (fleet momentarily busy) carry Retry-After."""
    for attempt in range(1, 11):
        t = time.time()
        async with http.post("https://starter.cdpfleet.com/chromium/session", headers={"x-api-key": KEY},
                             json={"proxy": os.environ["PROXY_URL"], "headless": True}) as res:
            body = await res.json(content_type=None)
            if res.status == 200:
                return body, (time.time() - t) * 1000
            retryable = res.status == 503 or (res.status == 429 and "quota" not in body.get("error", ""))
            if not retryable or attempt == 10:
                raise RuntimeError(f"launch: {res.status} {body.get('error')}")
            stats["retries"][body.get("error")] = stats["retries"].get(body.get("error"), 0) + 1
            await asyncio.sleep(float(res.headers.get("retry-after", 2)))


async def scrape(page, stats):
    # Residential proxies drop a tunnel now and then (ERR_TUNNEL_CONNECTION_FAILED): retry.
    for attempt in (1, 2, 3):
        try:
            await page.goto("https://en.wikipedia.org/wiki/Special:Random", timeout=30000)
            return await page.title()
        except Exception as err:
            stats["page_retries"] += 1
            if attempt == 3:
                return f"(failed: {str(err).splitlines()[0]})"


async def run(p, http, workers, reuse):
    """Runs PAGES pages on `workers` parallel workers; each worker either opens one session
    and reuses it, or opens a new session for every page."""
    stats = {"launches": 0, "launch_ms": 0, "connect_ms": 0, "session_s": 0, "titles": [], "retries": {}, "page_retries": 0, "connect_failures": 0}
    queue = asyncio.Queue()
    for i in range(PAGES):
        queue.put_nowait(i)

    async def open_session():
        # If the connect fails (rare: the server holding the browser didn't answer), don't
        # reconnect to the same wsUrl — launch a fresh session. Such sessions aren't billed.
        for attempt in (1, 2, 3):
            session, launch_ms = await launch(http, stats)
            stats["launches"] += 1
            stats["launch_ms"] += launch_ms
            t = time.time()
            try:
                browser = await p.chromium.connect(session["wsUrl"], headers={"x-api-key": KEY})
            except Exception:
                stats["connect_failures"] += 1
                if attempt == 3:
                    raise
                continue
            stats["connect_ms"] += (time.time() - t) * 1000
            return browser, t

    async def close_session(browser, started):
        await browser.close()
        stats["session_s"] += time.time() - started

    async def worker():
        if reuse:
            browser, started = await open_session()
            try:
                page = await browser.new_page()
                while not queue.empty():
                    queue.get_nowait()
                    stats["titles"].append(await scrape(page, stats))
            finally:
                await close_session(browser, started)
            return
        while not queue.empty():
            queue.get_nowait()
            browser, started = await open_session()
            try:
                stats["titles"].append(await scrape(await browser.new_page(), stats))
            finally:
                await close_session(browser, started)

    t = time.time()
    await asyncio.gather(*(worker() for _ in range(workers)))
    return {
        "strategy": "reuse one session per worker" if reuse else "new session per page",
        "pages": len(stats["titles"]),
        "wall_seconds": round(time.time() - t, 1),
        "launches": stats["launches"],
        "avg_launch_ms": round(stats["launch_ms"] / stats["launches"]),
        "avg_connect_ms": round(stats["connect_ms"] / stats["launches"]),
        "launch_retries": stats["retries"],
        "connect_failures": stats["connect_failures"],
        "page_retries": stats["page_retries"],
        "billed_thread_seconds": round(stats["session_s"]),
        "sample_titles": stats["titles"][:3],
    }


async def main():
    async with aiohttp.ClientSession() as http, async_playwright() as p:
        # Size the pool from the plan: never more workers than threads.
        async with http.get("https://cdpfleet.com/v1/me", headers={"x-api-key": KEY}) as r:
            threads = (await r.json())["subscription"]["threads"]
        workers = min(threads, 6)
        results = [await run(p, http, workers, False), await run(p, http, workers, True)]
        print(json.dumps({"plan_threads": threads, "workers": workers, "results": results}, indent=2))


asyncio.run(main())
