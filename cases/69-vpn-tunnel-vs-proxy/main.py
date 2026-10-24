# pip install playwright==1.60.0 requests
# env: CDPFLEET_API_KEY (sticky US exits from the cdpfleet residential proxy, Tier 1; one load per bot-protection page)
import json
import re
import secrets
import time
import os
from datetime import datetime, timezone

import requests
from playwright.sync_api import sync_playwright

KEY = os.environ["CDPFLEET_API_KEY"]
RUN = secrets.token_hex(3)


# One sticky exit per setup, used with and without the tunnel: the only difference between the two rows is vpn_tunnel.
def exit_for(sid):
    return f"cdpfleet-resi-country-us-session-t{sid}{RUN}-lifetime-30"


SETUPS = {
    "chrome": {"id": "c", "setup": "Google Chrome, headful", "engine": "chrome", "type": "chromium", "headless": False},
    "camoufox": {"id": "f", "setup": "Camoufox, defaults", "engine": "camoufox", "type": "firefox", "headless": True},
}
NETWORKS = {
    "proxy": {"label": "residential proxy", "body": {}},
    "tunnel": {"label": "residential proxy + vpn_tunnel", "body": {"vpn_tunnel": True}},
}

# The WebRTC leak test from case 54: ICE candidates against a public STUN server. A "srflx" candidate is the public IP
# the STUN server saw. The same expression runs in every language.
GATHER = r"""async () => {
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
# The protocol of the page's own request: h2 over TCP, or h3 over QUIC (UDP).
PROTOCOL = "performance.getEntriesByType('navigation')[0]?.nextHopProtocol || null"
# Cloudflare's interstitial or block page, or the real page.
CF_STATE = r"""(() => {
  const t = (document.title + ' ' + (document.body ? document.body.innerText : '').slice(0, 400)).replace(/\s+/g, ' ');
  return /sorry, you have been blocked|attention required|error 1020|access denied/i.test(t) ? 'blocked'
    : /just a moment|performing security verification|verify you are human|checking your browser/i.test(t) ? 'challenge' : 'page';
})()"""
# DataDome's iframe from captcha-delivery.com: a device check, a CAPTCHA, or a block (t=bv) — as in case 65.
DD_STATE = r"""(() => {
  const f = [...document.querySelectorAll('iframe')].map((i) => i.src).find((s) => /captcha-delivery\.com/.test(s)) || '';
  const text = (document.body ? document.body.innerText : '').trim();
  return /[?&]t=bv(&|$)/.test(f) ? 'blocked' : /\/captcha\//.test(f) ? 'challenge' : f ? 'checking' : text.length > 200 ? 'page' : 'loading';
})()"""
IPV4 = re.compile(r"^\d+\.\d+\.\d+\.\d+$")


# 503 means the fleet is momentarily busy: wait Retry-After and try again.
def launch(p, s, network):
    body = {"proxy": exit_for(s["id"]), "headless": s["headless"], "record": False, **NETWORKS[network]["body"]}
    for attempt in range(1, 7):
        res = requests.post(f"https://starter.cdpfleet.com/{s['engine']}/session", headers={"x-api-key": KEY}, json=body, timeout=120)
        if res.ok:
            return getattr(p, s["type"]).connect(res.json()["wsUrl"], headers={"x-api-key": KEY})
        if res.status_code not in (429, 503) or attempt == 6:
            raise RuntimeError(f"launch {res.status_code} {res.text}")
        time.sleep(int(res.headers.get("retry-after") or 5))


# Residential peers drop a connection now and then: one retry.
def go(tab, url):
    try:
        return tab.goto(url, wait_until="domcontentloaded", timeout=45000)
    except Exception:
        return tab.goto(url, wait_until="domcontentloaded", timeout=45000)


def evaluate(tab, expr):
    try:
        return tab.evaluate(expr)
    except Exception:
        return None


# One load, then watch the page once a second for 20 s (Cloudflare) or 12 s (DataDome) without touching it.
def watch(context, url, kind):
    tab = context.new_page()
    t0 = time.time()
    try:
        try:
            go(tab, url)
        except Exception:
            return {"outcome": "error", "seconds": None}
        seen = False
        until = time.time() + (20 if kind == "cf" else 12)
        while time.time() < until:
            s = evaluate(tab, CF_STATE if kind == "cf" else DD_STATE)
            if s == "blocked":
                return {"outcome": "blocked", "seconds": None}
            if s == "challenge" and kind == "dd":
                return {"outcome": "challenge", "seconds": None}
            if s in ("challenge", "checking"):
                seen = True
            if s == "page":
                return {"outcome": "cleared", "seconds": round(time.time() - t0)} if seen else {"outcome": "content", "seconds": None}
            time.sleep(1)
        return {"outcome": "challenge" if seen else "error", "seconds": None}
    finally:
        tab.close()


def row(p, setup_key, network):
    s = SETUPS[setup_key]
    browser = launch(p, s, network)
    out = {"setup": s["setup"], "network": NETWORKS[network]["label"]}
    try:
        context = browser.new_context()
        tab = context.new_page()
        go(tab, "https://api.ipify.org/?format=json")
        out["exit_ip"] = json.loads(tab.text_content("body"))["ip"]
        g = tab.evaluate(f"({GATHER})()")
        # candidate:<foundation> <component> <transport> <priority> <address> <port> typ <type> …
        parsed = [{"address": c.split(" ")[4], "type": c.split(" ")[7]} for c in g["candidates"]]
        out["webrtc_api"] = g["api"]
        ips = []
        for c in parsed:
            if IPV4.match(c["address"]) and c["address"] != "0.0.0.0" and c["address"] not in ips:
                ips.append(c["address"])
        out["webrtc_ips"] = ips
        out["mdns_only"] = not ips and any(c["address"].endswith(".local") for c in parsed)
        tab.close()
        # HTTP/3: a page announces it over HTTP/2 (Alt-Svc); the browser can switch to QUIC on a later request. Four loads
        # in fresh tabs, a pause between them.
        out["protocols"] = []
        for _ in range(4):
            t = context.new_page()
            try:
                go(t, "https://cloudflare-quic.com/")
            except Exception:
                pass
            out["protocols"].append(evaluate(t, PROTOCOL))
            t.close()
            time.sleep(2.5)
        context.close()
        # The bot-protection pages, each in a fresh context: Cloudflare's challenge test page (case 64) and DataDome's
        # own site (case 65).
        for key, url, kind in (("scrapingcourse", "https://www.scrapingcourse.com/cloudflare-challenge", "cf"), ("datadome", "https://datadome.co/", "dd")):
            c = browser.new_context()
            out[key] = watch(c, url, kind)
            c.close()
    finally:
        browser.close()
    return out


with sync_playwright() as p:
    rows = [row(p, s, n) for s in SETUPS for n in NETWORKS]
print(json.dumps({"measured_at": datetime.now(timezone.utc).strftime("%Y-%m-%d"), "rows": rows}, indent=2))
