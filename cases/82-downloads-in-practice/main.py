# pip install aiohttp mcp==2.3.0
# env: CDPFLEET_API_KEY (a paid plan: kept downloads aren't on Community), PROXY_URL
import asyncio
import hashlib
import json
import os
import re
import time
from urllib.parse import urlparse

import aiohttp
from mcp import ClientSession
from mcp.client.streamable_http import create_mcp_http_client, streamable_http_client

KEY = os.environ["CDPFLEET_API_KEY"]
PROXY = os.environ["PROXY_URL"]
API = "https://cdpfleet.com"

# Seeded test files: the same URL always returns the same bytes, so a copy can be checked
# against the original fetched directly from here.
PERSON_FILES = ["https://httpbingo.org/bytes/204800?seed=82", "https://httpbingo.org/bytes/65536?seed=7"]
AGENT_FILE = "https://httpbingo.org/bytes/102400?seed=79"


def now_ms():
    return round(time.monotonic() * 1000)


async def api(http, method, path):
    async with http.request(method, API + path, headers={"x-api-key": KEY}) as r:
        return r.status, await r.json()


async def listed(http, session_id, count, since):
    """Poll the account API until the session's kept files are listed; ms from `since`."""
    while True:
        _, body = await api(http, "GET", f"/v1/me/downloads?session_id={session_id}")
        if len(body["files"]) >= count or now_ms() - since > 120000:
            return body["files"], now_ms() - since
        await asyncio.sleep(0.25)


async def fetch_kept(http, files, links=None):
    """Fetch each kept file through its private link and compare it with what the list said and,
    for seeded files, with the original."""
    links = links or {}
    out = []
    for f in sorted(files, key=lambda f: -f["bytes"]):
        url, expires = links.get(f["name"]), None
        if not url:
            _, link = await api(http, "GET", f"/v1/me/downloads/{f['id']}/url")
            url, expires = link["url"], link["expires_in_s"]
        async with http.get(url) as r:
            got, status, ctype = await r.read(), r.status, r.headers.get("content-type")
        original = None
        if re.search(r"[?&]seed=", f.get("source_url") or ""):
            async with http.get(f["source_url"]) as r:
                original = await r.read()
        out.append({
            "name": f["name"], "bytes": f["bytes"], "sha256": f["sha256"], "source_url": f["source_url"],
            "link": "from the downloads tool" if f["name"] in links else f"GET /v1/me/downloads/{{id}}/url ({expires} s)",
            "fetched_status": status, "fetched_type": ctype, "fetched_bytes": len(got), "fetched_sha256": hashlib.sha256(got).hexdigest(),
            "original_bytes": len(original) if original is not None else None,
            "original_sha256": hashlib.sha256(original).hexdigest() if original is not None else None,
        })
    return out


# ---- 1. A person in Launch interactive: no client, the server opens the start page and
#         a person drives it through a control live link (a scripted stand-in here).
async def person(http):
    body = {"proxy": PROXY, "headless": True, "live_view": True, "record": False, "downloads": True,
            "start_url": "https://httpbingo.org/", "inactivity_timeout": 60}
    async with http.post("https://starter.cdpfleet.com/chromium/session", json=body, headers={"x-api-key": KEY}) as r:
        if not r.ok:
            raise RuntimeError(f"launch {r.status} {await r.text()}")
        session = await r.json()
    host = urlparse(session["wsUrl"]).netloc
    async with http.post(f"https://{host}/admin/session/live", json={"session_id": session["sessionId"], "mode": "control"},
                         headers={"x-api-key": KEY}) as r:
        live = await r.json()

    # The viewer: JPEG frames (binary) and JSON messages; "pages" says which URL is on screen.
    viewer = await http.ws_connect(live["live_url"], max_msg_size=0)
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
    for _ in range(60):
        if "https://httpbingo.org/" in seen:
            break
        await asyncio.sleep(0.5)
    start_shown = "https://httpbingo.org/" in seen

    # The person pastes two file links into the address bar, a few seconds apart.
    for url in PERSON_FILES:
        await viewer.send_str(json.dumps({"type": "navigate", "url": url}))
        await asyncio.sleep(3)
    page_after = seen[-1] if seen else None
    await viewer.close()
    reader.cancel()

    # Closing the dashboard's viewer ends a Launch interactive session: DELETE /v1/me/threads/{id}.
    t = now_ms()
    end_status, _ = await api(http, "DELETE", f"/v1/me/threads/{session['sessionId']}")
    ended = now_ms()
    files, ms = await listed(http, session["sessionId"], len(PERSON_FILES), ended)
    return {
        "launch_downloads": session.get("downloads"), "resource_class": session.get("resource_class"), "live_protocol": live["protocol"],
        "start_page_shown": start_shown, "frames": frames, "pasted": PERSON_FILES, "page_after": page_after,
        "end_status": end_status, "end_ms": ended - t, "listed_after_ms": ms, "files": await fetch_kept(http, files),
    }


# ---- 2. An AI agent through the hosted MCP server, authenticated with the API key.
async def agent(http):
    calls = []
    mcp_http = create_mcp_http_client(headers={"authorization": f"Bearer {KEY}"})
    async with mcp_http, streamable_http_client(f"{API}/mcp", http_client=mcp_http) as (read, write), ClientSession(read, write) as client:
        await client.initialize()

        async def call(name, args=None):
            t = now_ms()
            r = await client.call_tool(name, args or {})
            text = "\n".join(c.text for c in r.content if c.type == "text")
            calls.append({"tool": name, "ms": now_ms() - t, "error": bool(r.is_error), "text": text.split("\n")[0][:160]})
            return text

        launched = await call("launch_browser", {"browser": "chromium", "headless": True, "proxy": PROXY, "record": False, "downloads": True})
        m = re.search(r"session ([0-9a-f-]{36})", launched)
        session_id = m[1] if m else None
        await call("navigate", {"url": "https://httpbingo.org/"})
        await call("click", {"role": "link", "name": "/bytes/:n"})  # the index page's example link: 1,024 random bytes
        await call("navigate", {"url": AGENT_FILE})  # a file link opened directly
        await call("evaluate", {"expression": "location.href"})
        await asyncio.sleep(2)  # an assistant's next call comes seconds later; let the downloads finish
        await call("close_browser")

        # Ask the downloads tool until both files are there.
        closed = now_ms()
        text, polls = "", 0
        while now_ms() - closed < 120000:
            polls += 1
            text = await call("downloads")
            if len(re.findall(r" bytes, sha256 ", text)) >= 2:
                break
            await asyncio.sleep(0.5)
        tool_ms = now_ms() - closed

    # "<name> — <bytes> bytes, sha256 <hex>" and a private link on the next line.
    tool = [{"name": m[1], "bytes": int(m[2]), "sha256": m[3], "link": m[4]}
            for m in re.finditer(r"^(.+) — (\d+) bytes, sha256 ([0-9a-f]{64})\n {2}(https:\S+)", text, re.M)]
    files, _ = await listed(http, session_id, 2, now_ms())
    links = {f["name"]: f["link"] for f in tool}
    last = dict(calls[-1], polls=polls)
    return {
        "session_id_found": session_id is not None,
        "calls": [c for c in calls if c["tool"] != "downloads"] + [last],
        "tool_listed_after_ms": tool_ms,
        "first_downloads_call": next(c["text"] for c in calls if c["tool"] == "downloads"),
        "tool_files": [{"name": f["name"], "bytes": f["bytes"], "sha256": f["sha256"], "link_host": urlparse(f["link"]).netloc} for f in tool],
        "files": await fetch_kept(http, files, links),
    }


async def main():
    async with aiohttp.ClientSession() as http:  # no default headers: the key goes only to cdpfleet
        person_out = await person(http)
        agent_out = await agent(http)
    print(json.dumps({"language": "python", "person": person_out, "agent": agent_out,
                      "measured_at": time.strftime("%Y-%m-%d", time.gmtime())}, indent=2, ensure_ascii=False))


asyncio.run(main())
