# pip install playwright==1.60.0 requests
# env: CDPFLEET_API_KEY, PROXY_URL, PROXY_URL_DE, SOCKS_PROXIES (comma-separated socks5:// URLs)
import json
import os

import requests
from playwright.sync_api import sync_playwright

KEY = os.environ["CDPFLEET_API_KEY"]
ECHO = "wss://ws.postman-echo.com/raw"  # a public WebSocket echo server
PROXIES = {
    "residential (any country)": os.environ["PROXY_URL"],
    "residential (Germany)": os.environ["PROXY_URL_DE"],
    "datacenter SOCKS5": os.environ["SOCKS_PROXIES"].split(",")[0],
}

# Runs in the page: connect, then 20 echo round trips, one at a time.
PROBE = """async (url) => {
  const t0 = performance.now();
  const ws = new WebSocket(url);
  await new Promise((ok, fail) => { ws.onopen = ok; ws.onerror = () => fail(new Error('WebSocket failed')); });
  const connectMs = performance.now() - t0;
  const rtts = [];
  for (let i = 0; i < 20; i++) {
    const sent = performance.now();
    const echoed = new Promise((ok) => { ws.onmessage = (m) => ok(m.data); });
    ws.send(`ping ${i}`);
    if ((await echoed) !== `ping ${i}`) throw new Error('wrong echo');
    rtts.push(performance.now() - sent);
  }
  ws.close();
  rtts.sort((a, b) => a - b);
  return { connectMs, p50: rtts[10], p95: rtts[18], min: rtts[0] };
}"""


def measure_once(p, label, proxy):
    res = requests.post("https://starter.cdpfleet.com/chromium/session", headers={"x-api-key": KEY},
                        json={"proxy": proxy, "headless": True}, timeout=60)
    res.raise_for_status()
    browser = p.chromium.connect(res.json()["wsUrl"], headers={"x-api-key": KEY})
    try:
        page = browser.new_page()
        # Where this proxy exits (also gives the page an https origin to open the socket from).
        geo = page.goto("http://ip-api.com/json/?fields=country,city", timeout=30000).json()
        page.goto("https://httpbin.org/html", timeout=30000)
        r = page.evaluate(PROBE, ECHO)
        return {"proxy": label, "exit": f"{geo['city']}, {geo['country']}", "connect_ms": round(r["connectMs"]),
                "rtt_p50_ms": round(r["p50"]), "rtt_p95_ms": round(r["p95"]), "rtt_min_ms": round(r["min"])}
    finally:
        browser.close()


def measure(p, label, proxy):
    # Residential exits drop a connection now and then: one retry in a new session.
    for attempt in (1, 2):
        try:
            return measure_once(p, label, proxy)
        except Exception as err:
            if attempt == 2:
                return {"proxy": label, "error": str(err).splitlines()[0]}


with sync_playwright() as p:
    rows = [measure(p, label, proxy) for label, proxy in PROXIES.items()]
print(json.dumps({"echo_server": ECHO, "round_trips": 20, "results": rows}, indent=2))
