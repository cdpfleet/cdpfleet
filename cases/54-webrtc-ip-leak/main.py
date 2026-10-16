# pip install playwright==1.60.0 requests
# env: CDPFLEET_API_KEY (each setup gets its own sticky US exit from the cdpfleet residential proxy; a few KB of traffic)
import json
import os
import random
import re
import string
import time

import requests
from playwright.sync_api import sync_playwright

KEY = os.environ["CDPFLEET_API_KEY"]
# A sticky residential exit per setup: one IP for the whole session, to compare with.
RUN = "".join(random.choices(string.ascii_lowercase + string.digits, k=6))


def sticky_exit(i):
    return f"cdpfleet-resi-country-us-session-w54{RUN}{i}-lifetime-10"


# What a WebRTC leak test runs: gather ICE candidates against a public STUN server. A "srflx"
# candidate carries the public IP the STUN server saw; UDP doesn't go through an HTTP/SOCKS proxy —
# on cdpfleet, proxied sessions can't send UDP at all, so only an anonymous mDNS host candidate is left.
GATHER = """async () => {
  if (typeof RTCPeerConnection === 'undefined') return { api: false, candidates: [] };
  const pc = new RTCPeerConnection({ iceServers: [{ urls: 'stun:stun.l.google.com:19302' }] });
  const candidates = [];
  pc.onicecandidate = (e) => { if (e.candidate && e.candidate.candidate) candidates.push(e.candidate.candidate); };
  pc.createDataChannel('probe');
  await pc.setLocalDescription(await pc.createOffer());
  await new Promise((done) => {
    const timer = setTimeout(done, 8000);
    pc.onicegatheringstatechange = () => { if (pc.iceGatheringState === 'complete') { clearTimeout(timer); done(); } };
  });
  pc.close();
  return { api: true, candidates };
}"""

SETUPS = [
    {"setup": "Chromium", "engine": "chromium", "body": {"headless": "new"}},
    {"setup": "Google Chrome", "engine": "chrome", "body": {"headless": "new"}},
    {"setup": "Firefox", "engine": "firefox", "body": {"headless": True}},
    {"setup": "Camoufox (defaults: geoip on)", "engine": "camoufox", "body": {"headless": True}},
    {"setup": "Camoufox, block_webrtc: true", "engine": "camoufox", "body": {"headless": True, "block_webrtc": True}},
]


# 503 means the fleet is momentarily busy: wait Retry-After and try again.
def launch(engine, body):
    for attempt in range(1, 6):
        r = requests.post(f"https://starter.cdpfleet.com/{engine}/session", headers={"x-api-key": KEY}, json=body, timeout=120)
        if r.ok:
            return r.json()
        if r.status_code != 503 or attempt == 5:
            raise RuntimeError(f"launch {r.status_code} {r.text}")
        time.sleep(int(r.headers.get("retry-after") or 5))


# Residential peers drop a connection now and then: up to two retries.
def go(page, url):
    for attempt in range(1, 4):
        try:
            return page.goto(url, timeout=60000)
        except Exception:
            if attempt == 3:
                raise
            time.sleep(2)


results = []
with sync_playwright() as p:
    for i, s in enumerate(SETUPS):
        session = launch(s["engine"], {**s["body"], "proxy": sticky_exit(i), "record": False})
        browser_type = p.firefox if s["engine"] in ("firefox", "camoufox") else p.chromium
        browser = browser_type.connect(session["wsUrl"], headers={"x-api-key": KEY})
        try:
            context = browser.new_context()
            page = context.new_page()
            go(page, "https://api.ipify.org/?format=json")
            exit_ip = json.loads(page.text_content("body"))["ip"]
            gathered = page.evaluate(GATHER)
            # candidate:<foundation> <component> <transport> <priority> <address> <port> typ <type> …
            parsed = [{"address": c.split(" ")[4], "type": c.split(" ")[7]} for c in gathered["candidates"]]
            public_ips = list(dict.fromkeys(c["address"] for c in parsed if re.fullmatch(r"\d+\.\d+\.\d+\.\d+", c["address"]) and c["address"] != "0.0.0.0"))
            results.append({
                "setup": s["setup"],
                "exit_ip": exit_ip,
                "webrtc_api": gathered["api"],
                "candidates": len(gathered["candidates"]),
                "mdns_host_candidates": sum(1 for c in parsed if c["type"] == "host" and c["address"].endswith(".local")),
                "public_ips": public_ips,
                "shows_exit_ip": exit_ip in public_ips,
                "leaks_other_ip": any(ip != exit_ip for ip in public_ips),
            })
        finally:
            browser.close()
print(json.dumps(results, indent=2))
