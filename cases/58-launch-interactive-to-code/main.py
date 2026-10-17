# pip install playwright==1.60.0 aiohttp
# env: CDPFLEET_API_KEY, PROXY_URL (your own proxy; a few KB of traffic)
import asyncio
import json
import os
from datetime import datetime, timedelta, timezone
from urllib.parse import urlparse

import aiohttp
from playwright.async_api import async_playwright

KEY = os.environ["CDPFLEET_API_KEY"]
PROXY = os.environ["PROXY_URL"]
HEADERS = {"x-api-key": KEY}


# 503 means the fleet is momentarily busy: wait Retry-After and try again.
async def launch(http, engine, body):
    for attempt in range(1, 6):
        async with http.post(f"https://starter.cdpfleet.com/{engine}/session", json=body) as r:
            if r.ok:
                return await r.json()
            if r.status != 503 or attempt == 5:
                raise RuntimeError(f"launch {r.status} {await r.text()}")
            await asyncio.sleep(int(r.headers.get("retry-after", 5)))


def parse_time(s):
    return datetime.fromisoformat(s.replace("Z", "+00:00"))


# The account API's session history: how long each session ran and why it ended.
async def history(http, session_id):
    since = (datetime.now(timezone.utc) - timedelta(hours=1)).isoformat().replace("+00:00", "Z")
    for _ in range(30):
        async with http.get("https://cdpfleet.com/v1/me/sessions", params={"limit": "50", "from": since}) as r:
            rows = (await r.json())["sessions"]
        row = next((s for s in rows if s["session_id"] == session_id), None)
        if row and row.get("ended_at"):
            return {"seconds": round((parse_time(row["ended_at"]) - parse_time(row["started_at"])).total_seconds()), "end_reason": row["end_reason"]}
        await asyncio.sleep(2)
    return None


async def main():
    async with aiohttp.ClientSession(headers=HEADERS) as http, async_playwright() as p:
        # 1. Launch with a start page and no client: the browser opens it by itself. A twin session
        #    with the same options gets no viewer at all, to compare with.
        body = {"proxy": PROXY, "headless": True, "live_view": True, "record": False,
                "start_url": "https://httpbin.org/", "inactivity_timeout": "30s"}
        session = await launch(http, "chromium", body)
        twin = await launch(http, "chromium", body)

        # 2. A live link in control mode — what "Launch interactive" in the dashboard opens for you.
        host = urlparse(session["wsUrl"]).netloc
        async with http.post(f"https://{host}/admin/session/live", json={"session_id": session["sessionId"], "mode": "control"}) as r:
            live = await r.json()

        # The viewer: JPEG frames (binary) and JSON messages; "pages" says which URL is on screen.
        viewer = await http.ws_connect(live["live_url"], headers={}, max_msg_size=0)
        seen, frames = [], 0

        async def read():
            nonlocal frames
            async for msg in viewer:
                if msg.type == aiohttp.WSMsgType.BINARY:
                    frames += 1
                elif msg.type == aiohttp.WSMsgType.TEXT:
                    m = json.loads(msg.data)
                    if m.get("type") == "pages":
                        url = next((pg["url"] for pg in m["pages"] if pg["targetId"] == m["current"]), None)
                        if url and (not seen or seen[-1] != url):
                            seen.append(url)

        reader = asyncio.create_task(read())

        async def send(m):
            await viewer.send_str(json.dumps(m))

        async def wait_for_url(part):
            for _ in range(60):
                if any(part in u for u in seen):
                    return True
                await asyncio.sleep(0.5)
            return any(part in u for u in seen)

        async def key(k):
            await send({"type": "key", "action": "keyDown", "key": k, "code": k})
            await send({"type": "key", "action": "keyUp", "key": k, "code": k})

        # 3. The person (here a scripted stand-in): goes to the form from the address bar, then fills
        #    the first three fields from the keyboard — Tab to a field, type.
        start_shown = await wait_for_url("httpbin.org")
        await send({"type": "navigate", "url": "https://httpbin.org/forms/post"})
        form_shown = await wait_for_url("/forms/post")
        await asyncio.sleep(1.5)
        for text in ["Ada Lovelace", "+44 20 7946 0000", "ada@example.com"]:
            await key("Tab")
            await asyncio.sleep(0.2)
            await send({"type": "text", "text": text})
            await asyncio.sleep(0.3)
        # 4. Then only watches — no input for 45 s, past the 30 s inactivity_timeout.
        await asyncio.sleep(45)

        # The unwatched twin has had no input and no viewer for as long: it should be gone.
        twin_alive = True
        try:
            await (await p.chromium.connect(twin["wsUrl"], headers=HEADERS, timeout=15000)).close()
        except Exception:
            twin_alive = False

        # 5. Your code joins the same browser and finds the person's page in the default context.
        browser = await p.chromium.connect(session["wsUrl"], headers=HEADERS)
        out = {
            "live_protocol": live["protocol"], "start_page_shown": start_shown, "form_shown": form_shown, "frames_while_by_hand": frames,
            "watched_session_alive_after_45s": browser.is_connected(), "unwatched_twin_alive_after_45s": twin_alive,
        }
        try:
            pages = browser.contexts[0].pages
            page = next(pg for pg in pages if "/forms/post" in pg.url)
            out["pages_found"] = [pg.url for pg in pages]
            out["typed_by_hand"] = {name: await page.input_value(f"input[name={name}]") for name in ["custname", "custtel", "custemail"]}
            # 6. The code finishes the job: the choices a script makes reliably, then submit.
            await page.check("input[name=size][value=medium]")
            await page.check("input[name=topping][value=cheese]")
            await page.fill("input[name=delivery]", "19:30")
            async with page.expect_navigation(url="**/post"):
                await page.click("form button")
            out["submitted"] = json.loads(await page.text_content("body"))["form"]
            await asyncio.sleep(1.5)
            out["viewer_saw"] = seen
        finally:
            await viewer.close()
            reader.cancel()
            await browser.close()

        # 7. The account API's session history.
        out["watched_session"] = await history(http, session["sessionId"])
        out["unwatched_twin"] = await history(http, twin["sessionId"])
        print(json.dumps(out, indent=2))


asyncio.run(main())
