# pip install mcp==2.3.0
# env: CDPFLEET_API_KEY, PROXY_URL
import asyncio
import json
import os
import re
import time

from mcp import ClientSession
from mcp.client.streamable_http import create_mcp_http_client, streamable_http_client

MCP = "https://cdpfleet.com/mcp"
KEY = os.environ["CDPFLEET_API_KEY"]
PROXY = os.environ["PROXY_URL"]
EXTRACT = ("() => [...document.querySelectorAll('article.product_pod')].map((a) => ({ title: a.querySelector('h3 a').title,"
           " price: a.querySelector('.price_color').textContent }))")


def ms(t):
    return round((time.monotonic() - t) * 1000)


class Connection:
    """One MCP session over Streamable HTTP; a script authenticates with its API key as a bearer token."""

    def __init__(self, terminate):
        self.terminate = terminate
        self.session_id = None

    async def __aenter__(self):
        http = create_mcp_http_client(headers={"authorization": f"Bearer {KEY}"})

        async def remember(response):  # the server's MCP session id, from its response header
            self.session_id = response.headers.get("mcp-session-id") or self.session_id

        http.event_hooks["response"].append(remember)
        self._http = http
        self._streams = streamable_http_client(MCP, http_client=http, terminate_on_close=self.terminate)
        read, write = await self._streams.__aenter__()
        self._session = ClientSession(read, write)
        self.client = await self._session.__aenter__()
        await self.client.initialize()
        return self

    async def __aexit__(self, *exc):
        await self._session.__aexit__(*exc)
        await self._streams.__aexit__(*exc)
        await self._http.aclose()


async def call(client, log, name, args=None):
    t = time.monotonic()
    r = await client.call_tool(name, args or {})
    text = "\n".join(c.text for c in r.content if c.type == "text")
    log.append({"tool": name, "ms": ms(t), "error": bool(r.is_error)})
    return text, bool(r.is_error)


async def navigate(client, log, url):
    """A shared proxy drops the odd connection: one retry of a failed page load."""
    text, err = await call(client, log, "navigate", {"url": url})
    return await call(client, log, "navigate", {"url": url}) if err else (text, err)


# ---- 1. One MCP session for the whole job, as Claude or Cursor keep one.
async def one_session():
    log, out = [], {}
    t = time.monotonic()
    async with Connection(terminate=True) as c:
        log.append({"tool": "initialize", "ms": ms(t), "error": False})
        t = time.monotonic()
        tools = (await c.client.list_tools()).tools
        log.append({"tool": "tools/list", "ms": ms(t), "error": False})
        out["mcp_session"] = c.session_id is not None
        out["tools"] = sorted(x.name for x in tools)

        text, _ = await call(c.client, log, "launch_browser",
                             {"browser": "chromium", "headless": True, "proxy": PROXY, "record": False})
        m = re.match(r"^Launched (\S+) (\S+) \((\w+), (\d+) thread", text)
        out.update(browser=m and m[1], version=m and m[2], resource_class=m and m[3], weight=int(m[4]) if m else None)
        sid = re.search(r"session ([0-9a-f-]{36})", text)
        out["session_id"] = sid[1] if sid else None
        out["navigate"] = (await navigate(c.client, log, "https://books.toscrape.com/"))[0].split("\n")[0]
        out["snapshot_chars"] = len((await call(c.client, log, "snapshot"))[0])
        out["snapshot_list_chars"] = len((await call(c.client, log, "snapshot", {"selector": "ol.row"}))[0])
        out["html_chars"] = int((await call(c.client, log, "evaluate", {"expression": "document.documentElement.outerHTML.length"}))[0])
        books = json.loads((await call(c.client, log, "evaluate", {"expression": EXTRACT}))[0])
        out["books"] = len(books)
        out["first_book"] = books[0]
        closed, _ = await call(c.client, log, "close_browser")
        out["closed"] = closed.startswith(f"Session {out['session_id']} ended")
        text, err = await call(c.client, log, "navigate", {"url": "https://books.toscrape.com/"})
        out["after_close"] = {"error": err, "text": text.split("\n")[0]}
        out["live_sessions_after"] = json.loads((await call(c.client, log, "account"))[0])["live_sessions"]
    out["calls"] = log
    return out


# ---- 2. A new MCP session for every tool call, as ChatGPT does.
async def per_call_sessions():
    log, sessions = [], []

    async def once(name, args):
        t = time.monotonic()
        async with Connection(terminate=False) as c:  # no DELETE: the client just goes away
            text, err = await (navigate(c.client, log, args["url"]) if name == "navigate" else call(c.client, log, name, args))
            sessions.append(c.session_id)
        log[-1]["with_initialize_ms"] = ms(t)
        return text

    launched = await once("launch_browser", {"browser": "chromium", "headless": True, "proxy": PROXY, "record": False})
    sid = re.search(r"session ([0-9a-f-]{36})", launched)
    browser_session = sid[1] if sid else None
    nav = await once("navigate", {"url": "https://books.toscrape.com/catalogue/page-2.html"})
    where = await once("evaluate", {"expression": '({ url: location.href, books: document.querySelectorAll("article.product_pod").length })'})
    closed = await once("close_browser", {})
    return {
        "mcp_sessions": len(sessions),
        "distinct_mcp_sessions": len(set(sessions)),
        "browser_session_id": browser_session,
        "navigate": nav.split("\n")[0],
        "later_session_sees": json.loads(where),
        "closed_same_browser": closed.startswith(f"Session {browser_session} ended"),
        "calls": log,
    }


async def main():
    one = await one_session()
    per_call = await per_call_sessions()
    print(json.dumps({"language": "python", "one_session": one, "per_call": per_call,
                      "measured_at": time.strftime("%Y-%m-%d", time.gmtime())}, indent=2, ensure_ascii=False))


asyncio.run(main())
