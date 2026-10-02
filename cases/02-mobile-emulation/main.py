# pip install playwright==1.60.0 requests
# env: CDPFLEET_API_KEY, PROXY_URL
import json
import os

import requests
from playwright.sync_api import sync_playwright

KEY = os.environ["CDPFLEET_API_KEY"]

res = requests.post("https://starter.cdpfleet.com/chrome/session", headers={"x-api-key": KEY},
                    json={"proxy": os.environ["PROXY_URL"], "headless": True}, timeout=60)
res.raise_for_status()
ws_url = res.json()["wsUrl"]

PAGE_SIGNALS = """() => ({
  viewport: `${innerWidth}x${innerHeight}`,
  screen: `${screen.width}x${screen.height}`,
  device_pixel_ratio: devicePixelRatio,
  max_touch_points: navigator.maxTouchPoints,
  coarse_pointer: matchMedia('(pointer: coarse)').matches,
  platform: navigator.platform,
  ua_data_mobile: navigator.userAgentData ? navigator.userAgentData.mobile : null,
  ua_data_platform: navigator.userAgentData ? navigator.userAgentData.platform : null,
})"""


def inspect(context):
    """What a page can see about the device, plus what the network sees (tls.peet.ws)."""
    page = context.new_page()
    fp = page.goto("https://tls.peet.ws/api/all", timeout=60000).json()
    js = page.evaluate(PAGE_SIGNALS)
    headers = next(f for f in fp["http2"]["sent_frames"] if f["frame_type"] == "HEADERS")["headers"]
    header = lambda name: next((h[len(name) + 2:] for h in headers if h.startswith(f"{name}: ")), None)
    page.close()
    return {
        "user_agent": fp["user_agent"],
        **js,
        "sec_ch_ua_mobile": header("sec-ch-ua-mobile"),
        "sec_ch_ua_platform": header("sec-ch-ua-platform"),
        "ja4": fp["tls"]["ja4"],
        "akamai_h2_hash": fp["http2"]["akamai_fingerprint_hash"],
    }


with sync_playwright() as p:
    browser = p.chromium.connect(ws_url, headers={"x-api-key": KEY})
    try:
        print(json.dumps({
            "desktop": inspect(browser.new_context()),
            "iPhone 15 Pro": inspect(browser.new_context(**p.devices["iPhone 15 Pro"])),
            "Pixel 7": inspect(browser.new_context(**p.devices["Pixel 7"])),
        }, indent=2))
    finally:
        browser.close()
