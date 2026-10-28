# pip install playwright==1.60.0 requests
# env: CDPFLEET_API_KEY (residential exits come from cdpfleet-resi tokens)
import json
import os
import time
from concurrent.futures import ThreadPoolExecutor

import requests
from playwright.sync_api import sync_playwright

KEY = os.environ["CDPFLEET_API_KEY"]
RUN = format(int(time.time() * 1000), "x")[-4:]
COUNTRIES = [
    {"country": "US", "locale": "en-US"},
    {"country": "DE", "locale": "de-DE"},
    {"country": "JP", "locale": "ja-JP"},
]
# (label, engine, launch options, context options from the exit: "match" = exit's locale + timezone)
SETUPS = [
    ("Camoufox (geoip on, default)", "camoufox", {"geoip": True}, None),
    ("Camoufox, geoip: false", "camoufox", {"geoip": False}, None),
    ('Camoufox, locale: "pt-BR"', "camoufox", {"geoip": True, "locale": "pt-BR"}, None),
    ("Chrome headful, no locale", "chrome", {"headless": False}, None),
    ("Chrome headful, context locale + timezoneId from the exit", "chrome", {"headless": False}, "match"),
]

# What a site can read and compare, run on the exit-lookup page itself (ip-api.com's JSON, so the
# page knows the exit): exit country and timezone, languages, Accept-Language (echoed by httpbin),
# Intl's timezone and locale, a formatted number, and the WebRTC public address (compared with the
# exit, never returned).
CHECK = """(async () => {
  const exit = JSON.parse(document.body.innerText);
  const headers = await fetch('https://httpbin.org/headers').then((r) => r.json()).catch(() => ({ headers: {} }));
  const srflx = await new Promise((done) => {
    const ips = new Set();
    try {
      const pc = new RTCPeerConnection({ iceServers: [{ urls: 'stun:stun.l.google.com:19302' }] });
      pc.createDataChannel('x');
      pc.onicecandidate = (e) => { const c = e.candidate && e.candidate.candidate; if (c && c.includes(' srflx ')) ips.add(c.split(' ')[4]); };
      pc.createOffer().then((o) => pc.setLocalDescription(o));
      setTimeout(() => { pc.close(); done([...ips]); }, 4000);
    } catch { done([]); }
  });
  const intl = Intl.DateTimeFormat().resolvedOptions();
  const acceptLanguage = (headers.headers['Accept-Language'] || '').split(',')[0];
  const out = {
    exit_country: exit.countryCode, exit_timezone: exit.timezone,
    languages: navigator.languages.join(', '), accept_language: acceptLanguage,
    timezone: intl.timeZone, intl_locale: intl.locale, number: (1234567.891).toLocaleString(),
    webrtc: srflx.length === 0 ? 'none' : srflx.every((ip) => ip === exit.query) ? 'exit IP' : 'another IP',
  };
  out.mismatches = [
    out.timezone !== exit.timezone && 'timezone ≠ IP timezone',
    (navigator.language.split('-')[1] || '').toUpperCase() !== exit.countryCode && 'language region ≠ IP country',
    acceptLanguage.toLowerCase() !== navigator.language.toLowerCase() && 'Accept-Language ≠ navigator.language',
    !intl.locale.toLowerCase().startsWith(navigator.language.slice(0, 2).toLowerCase()) && 'Intl locale ≠ navigator.language',
    out.webrtc === 'another IP' && 'WebRTC shows another IP',
  ].filter(Boolean);
  return out;
})()"""


def measure(c, setup):
    label, engine, options, context_from = setup
    cc = c["country"].lower()
    proxy = f"cdpfleet-resi-country-{cc}-session-c74{cc}{RUN}-lifetime-10"
    row = {"setup": label, "country": c["country"]}
    camoufox = engine == "camoufox"
    body = {"proxy": proxy, "headless": camoufox, "record": False, **({"main_world_eval": True} if camoufox else {}), **options}
    res = requests.post(f"https://starter.cdpfleet.com/{engine}/session", headers={"x-api-key": KEY}, json=body, timeout=180)
    if not res.ok:
        return {**row, "error": f"launch {res.status_code} {res.text[:120]}"}
    with sync_playwright() as p:  # the sync API is per thread
        browser = (p.firefox if camoufox else p.chromium).connect(res.json()["wsUrl"], headers={"x-api-key": KEY})
        try:
            context_options = {}
            if context_from == "match":  # look the exit up first, then set the context to match it
                probe = browser.new_context()
                pg = probe.new_page()
                pg.goto("http://ip-api.com/json/?fields=timezone", timeout=60000)
                context_options = {"locale": c["locale"], "timezone_id": json.loads(pg.text_content("body"))["timezone"]}
                probe.close()
            page = browser.new_context(**context_options).new_page()
            page.goto("http://ip-api.com/json/?fields=countryCode,timezone,query", timeout=60000)
            row.update(page.evaluate(f"mw:{CHECK}" if camoufox else CHECK))
        except Exception as err:  # noqa: BLE001 — report the cell, keep the run going
            row["error"] = str(err).split("\n")[0]
        finally:
            browser.close()
    return row


# Countries in parallel, setups one after another within a country (one exit per country).
def country_rows(c):
    return [measure(c, s) for s in SETUPS]


with ThreadPoolExecutor(len(COUNTRIES)) as pool:
    print(json.dumps([r for rows in pool.map(country_rows, COUNTRIES) for r in rows], indent=2, ensure_ascii=False))
