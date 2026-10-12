# pip install playwright==1.60.0 requests
# env: CDPFLEET_API_KEY, PROXY_URL_DE (a German proxy), PROXY_URL_US (a US proxy)
import json
import os
import time
from urllib.parse import urlparse

import requests
from playwright.sync_api import sync_playwright

KEY = os.environ["CDPFLEET_API_KEY"]
SITES = ["https://www.theguardian.com/international", "https://www.bbc.com/", "https://apnews.com/", "https://www.theverge.com/", "https://www.huffpost.com/"]
# First-layer buttons of the two consent platforms these sites use. OneTrust renders in the
# page; Sourcepoint in an iframe. Only a free "reject" counts: "Reject all and subscribe" doesn't.
REJECT = {"OneTrust": "#onetrust-reject-all-handler", "Sourcepoint": 'button[title="Reject all"], button[title="I do not agree"]'}
ACCEPT = {"OneTrust": "#onetrust-accept-btn-handler", "Sourcepoint": 'button[title="Accept all"], button[title="I agree"]'}
SP_FRAME = 'iframe[id^="sp_message_iframe"]'
BANNER = '#onetrust-banner-sdk, iframe[id^="sp_message_iframe"]'

# Which consent platform the page runs, whether its banner is showing, and which consent
# APIs it exposes (__tcfapi: IAB Europe's TCF; __gpp: IAB Tech Lab's GPP, used for US state
# privacy laws).
PROBE = """() => {
  const shown = (s) => [...document.querySelectorAll(s)].some((e) => e.getClientRects().length && getComputedStyle(e).visibility !== 'hidden');
  const cmp = window.OneTrust || document.querySelector('#onetrust-consent-sdk') ? 'OneTrust'
    : window._sp_ || document.querySelector('iframe[id^="sp_message_iframe"]') ? 'Sourcepoint'
    : window.Didomi ? 'Didomi' : window.Cookiebot ? 'Cookiebot' : window.UC_UI ? 'Usercentrics' : null;
  return { cmp, banner: shown('#onetrust-banner-sdk, iframe[id^="sp_message_iframe"]'),
    tcf: typeof window.__tcfapi === 'function', gpp: typeof window.__gpp === 'function' };
}"""


def launch(p, proxy):
    res = requests.post("https://starter.cdpfleet.com/chromium/session",
        headers={"x-api-key": KEY}, json={"proxy": proxy, "headless": False}, timeout=60)
    if not res.ok:
        raise RuntimeError(f"launch {res.status_code} {res.text}")
    return p.chromium.connect(res.json()["wsUrl"], headers={"x-api-key": KEY})


def on_domain(host, domain):
    return host == domain or host.endswith("." + domain)


def press(button):
    """Clicks a consent button; True if the click went through (one retry: a consent iframe that
    re-renders can swallow a click)."""
    for _ in range(2):
        try:
            button.click(timeout=10000)
            return True
        except Exception:
            pass
    return False


def visit(browser, url, choice=None):
    """Load a site in a fresh context, then optionally click a consent button. Counts cookies and
    the hosts on other domains the page contacted (ads, analytics, CDNs) up to each moment."""
    domain = ".".join(urlparse(url).hostname.split(".")[-2:])
    ctx = browser.new_context(viewport={"width": 1280, "height": 900})
    page = ctx.new_page()
    hosts = set()

    def on_request(r):
        h = urlparse(r.url).hostname or ""
        if h and not on_domain(h, domain):
            hosts.add(h)
    page.on("request", on_request)

    def measure():
        cookies = ctx.cookies()
        return {"cookies": len(cookies), "third_party_cookies": len([c for c in cookies if not on_domain(c["domain"].lstrip("."), domain)]), "other_hosts": len(hosts)}

    def open_page():
        return page.goto(url, wait_until="domcontentloaded", timeout=60000)

    try:
        try:
            res = open_page()
        except Exception:  # proxies drop a connection now and then: one retry
            res = open_page()
        # Consent scripts load late on busy pages: wait up to 20 s for the banner, and give
        # every page at least 6 s before measuring.
        t0 = time.monotonic()
        try:
            page.locator(BANNER).first.wait_for(state="visible", timeout=20000)
            banner_after_ms = round((time.monotonic() - t0) * 1000)
        except Exception:
            banner_after_ms = None
        elapsed = (time.monotonic() - t0) * 1000
        if elapsed < 6000:
            page.wait_for_timeout(6000 - elapsed)
        probe = page.evaluate(f"({PROBE})()")
        before = {"status": res.status if res else None, **probe, "banner_after_ms": banner_after_ms, **measure()}
        if not choice:
            return {"before": before}
        missing = f"no {choice} button on the first layer" if probe["banner"] else "no banner"
        selector = (REJECT if choice == "reject" else ACCEPT).get(probe["cmp"])
        if not selector:
            return {"before": before, "after": None, "missing": missing}
        scope = page.frame_locator(SP_FRAME) if probe["cmp"] == "Sourcepoint" else page
        button = scope.locator(selector).first
        try:
            button.wait_for(state="visible", timeout=10000)
        except Exception:
            return {"before": before, "after": None, "missing": missing}
        label = button.get_attribute("title") or button.inner_text().strip()
        page.wait_for_timeout(2000)  # let the banner finish animating in, or the click can be lost
        if not press(button):
            return {"before": before, "after": None, "missing": "click failed"}
        clicks = 1
        try:  # if the banner is still up, the click was lost: click once more
            page.locator(BANNER).first.wait_for(state="hidden", timeout=5000)
        except Exception:
            clicks = 2
            press(button)
        page.wait_for_timeout(6000)
        return {"before": before, "after": {"button": label, "clicks": clicks, **measure()}}
    finally:
        ctx.close()


def visit_once(browser, url, choice=None):
    """A site may refuse one exit IP (an error status): visit again in a fresh context, whose new
    proxy connection gets another IP from the rotating proxy."""
    r = visit(browser, url, choice)
    status = r["before"]["status"]
    return r if status and 200 <= status < 300 else visit(browser, url, choice)


with sync_playwright() as p:
    # Sessions on shared threads end after 60 s without traffic, so the countries run one after the other.
    us = launch(p, os.environ["PROXY_URL_US"])
    try:
        from_us = [visit_once(us, url) for url in SITES]
    finally:
        us.close()
    sites = []
    de = launch(p, os.environ["PROXY_URL_DE"])
    try:
        for i, url in enumerate(SITES):
            rejected = visit_once(de, url, "reject")
            accepted = visit_once(de, url, "accept")
            sites.append({"site": urlparse(url).hostname.removeprefix("www."), "us": from_us[i]["before"], "de": rejected["before"],
                          "de_reject": rejected["after"], "de_reject_missing": rejected.get("missing"),
                          "de_accept": accepted["after"], "de_accept_missing": accepted.get("missing")})
    finally:
        de.close()
    print(json.dumps({"sites": sites}, indent=2))
