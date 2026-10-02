# pip install playwright==1.60.0 requests
# env: CDPFLEET_API_KEY, PROXY_URL (any exit), PROXY_URL_DE (an exit in Germany)
import json
import os

import requests
from playwright.sync_api import sync_playwright

KEY = os.environ["CDPFLEET_API_KEY"]


def persona(proxy):
    # A German Windows desktop: every value below is part of one consistent story.
    return {
        "proxy": proxy,
        "headless": True,
        "os": "windows",
        "locale": "de-DE",
        "screen": {"minWidth": 1920, "maxWidth": 1920, "minHeight": 1080, "maxHeight": 1080},
        "window": [1600, 900],
        "humanize": True,
        "block_webrtc": True,
        "geoip": True,  # timezone and geolocation follow the proxy's exit IP
    }


PAGE_SIGNALS = """() => {
  const gl = document.createElement('canvas').getContext('webgl');
  const dbg = gl && gl.getExtension('WEBGL_debug_renderer_info');
  return {
    platform: navigator.platform,
    languages: navigator.languages,
    timezone: Intl.DateTimeFormat().resolvedOptions().timeZone,
    screen: `${screen.width}x${screen.height}`,
    window: `${outerWidth}x${outerHeight}`,
    hardware_concurrency: navigator.hardwareConcurrency,
    webgl_renderer: dbg ? gl.getParameter(dbg.UNMASKED_RENDERER_WEBGL) : null,
    webrtc: typeof RTCPeerConnection !== 'undefined',
  };
}"""


def run(p, proxy):
    res = requests.post("https://starter.cdpfleet.com/camoufox/session", headers={"x-api-key": KEY},
                        json=persona(proxy), timeout=60)
    res.raise_for_status()
    browser = p.firefox.connect(res.json()["wsUrl"], headers={"x-api-key": KEY})
    try:
        page = browser.new_page()
        fp = page.goto("https://tls.peet.ws/api/all", timeout=60000).json()
        seen = page.evaluate(PAGE_SIGNALS)
        headers = next(f for f in fp["http2"]["sent_frames"] if f["frame_type"] == "HEADERS")["headers"]
        # Where the proxy exits, as a website would look it up.
        geo = page.goto("http://ip-api.com/json/?fields=country,timezone", timeout=60000).json()
        return {
            "exit_country": geo["country"],
            "exit_timezone": geo["timezone"],
            "user_agent": fp["user_agent"],
            "accept_language": next((h[17:] for h in headers if h.startswith("accept-language: ")), None),
            **seen,
            "ja4": fp["tls"]["ja4"],
        }
    finally:
        browser.close()


with sync_playwright() as p:
    print(json.dumps({
        "random exit": run(p, os.environ["PROXY_URL"]),
        "German exit": run(p, os.environ["PROXY_URL_DE"]),
    }, indent=2))
