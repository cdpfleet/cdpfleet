# pip install playwright==1.60.0 requests
# env: CDPFLEET_API_KEY, PROXY_URL
import datetime
import json
import os
import re
from urllib.parse import urlparse

import requests
from playwright.sync_api import sync_playwright

KEY = os.environ["CDPFLEET_API_KEY"]
# Public pages, each loaded once. `demo` pages are the protection vendors' own test pages and are
# reported by name; for the rest only the counts per vendor are reported. `expect` is the vendor a
# first survey (2026-10-19) found on that page.
PAGES = [
    {"url": "https://nowsecure.nl/", "expect": "turnstile", "demo": "nowsecure.nl (Turnstile test page)"},
    {"url": "https://www.scrapingcourse.com/cloudflare-challenge", "expect": "cloudflare", "demo": "scrapingcourse.com Cloudflare challenge"},
    {"url": "https://peet.ws/turnstile-test/non-interactive.html", "expect": "turnstile", "demo": "peet.ws Turnstile test (non-interactive)"},
    {"url": "https://recaptcha-demo.appspot.com/recaptcha-v3-request-scores.php", "expect": "recaptcha", "demo": "Google reCAPTCHA v3 demo"},
    {"url": "https://accounts.hcaptcha.com/demo", "expect": "hcaptcha", "demo": "hCaptcha demo"},
    {"url": "https://www.crunchbase.com/", "expect": "cloudflare"},
    {"url": "https://discord.com/", "expect": "cloudflare"},
    {"url": "https://www.glassdoor.com/", "expect": "cloudflare"},
    {"url": "https://www.tripadvisor.com/", "expect": "datadome"},
    {"url": "https://www.etsy.com/", "expect": "datadome"},
    {"url": "https://soundcloud.com/", "expect": "datadome"},
    {"url": "https://www.homedepot.com/", "expect": "akamai"},
    {"url": "https://www.usps.com/", "expect": "akamai"},
    {"url": "https://www.delta.com/", "expect": "akamai"},
    {"url": "https://www.zillow.com/", "expect": "human"},
    {"url": "https://www.walmart.com/", "expect": "human"},
    {"url": "https://www.realestate.com.au/", "expect": "kasada"},
    {"url": "https://www.ticketmaster.com/", "expect": "kasada"},
    {"url": "https://www.amazon.com/", "expect": "awswaf"},
    {"url": "https://www.booking.com/", "expect": "awswaf"},
]
VENDORS = ["cloudflare", "turnstile", "datadome", "akamai", "human", "kasada", "imperva", "awswaf", "recaptcha", "hcaptcha"]


def host(u):
    try:
        return urlparse(u).hostname or ""
    except ValueError:
        return ""


# (vendor, signal label, test) — each vendor leaves response headers, cookies or requests to its own hosts.
SIGNALS = [
    ("cloudflare", "cf-ray header", lambda h, c, u: "cf-ray" in h),
    ("cloudflare", "__cf_bm / cf_clearance cookie", lambda h, c, u: any(x in ("__cf_bm", "cf_clearance", "__cflb") for x in c)),
    ("cloudflare", "/cdn-cgi/challenge-platform request", lambda h, c, u: any("/cdn-cgi/challenge-platform" in x for x in u)),
    ("turnstile", "Turnstile widget", lambda h, c, u: any("challenges.cloudflare.com" in x and "turnstile" in x for x in u)),
    ("datadome", "x-datadome header", lambda h, c, u: "x-datadome" in h or "x-dd-b" in h),
    ("datadome", "datadome cookie", lambda h, c, u: "datadome" in c),
    ("datadome", "DataDome host (captcha-delivery.com, datadome.co)", lambda h, c, u: any(re.search(r"captcha-delivery\.com|datadome\.co", host(x)) for x in u)),
    ("akamai", "_abck / bm_sz cookie", lambda h, c, u: any(x in ("_abck", "bm_sz", "ak_bmsc", "bm_sv") for x in c)),
    ("akamai", "akamai header", lambda h, c, u: any(k.startswith("akamai") or k.startswith("x-akamai") for k in h)),
    ("human", "_px cookie", lambda h, c, u: any(x.startswith("_px") for x in c)),
    ("human", "HUMAN host (px-cdn.net, px-cloud.net)", lambda h, c, u: any(re.search(r"px-cdn\.net|px-cloud\.net|perimeterx\.net|pxchk\.net", host(x)) for x in u)),
    ("kasada", "x-kpsdk header", lambda h, c, u: any(k.startswith("x-kpsdk") for k in h)),
    ("kasada", "ips.js / kpsdk request", lambda h, c, u: any("/ips.js" in x or "kpsdk" in x for x in u)),
    ("imperva", "x-iinfo header", lambda h, c, u: "x-iinfo" in h),
    ("imperva", "incap_ses / visid_incap cookie", lambda h, c, u: any(re.match(r"^(incap_ses|visid_incap|nlbi_)", x) for x in c)),
    ("awswaf", "aws-waf-token cookie", lambda h, c, u: "aws-waf-token" in c),
    ("awswaf", "AWS WAF host (awswaf.com)", lambda h, c, u: any(re.search(r"awswaf\.com", host(x)) for x in u)),
    ("recaptcha", "reCAPTCHA script", lambda h, c, u: any("google.com/recaptcha" in x or "recaptcha.net" in x for x in u)),
    ("hcaptcha", "hCaptcha host", lambda h, c, u: any(re.search(r"hcaptcha\.com", host(x)) for x in u)),
]
CHALLENGE = re.compile(r"just a moment|checking your browser|verify you are human|press & hold|press and hold|are you a robot|human touch", re.I)
BLOCKED = re.compile(r"access denied|access to this page has been denied|attention required|you have been blocked|request blocked|error 1020|pardon our interruption", re.I)

res = requests.post(
    "https://starter.cdpfleet.com/chromium/session",
    headers={"x-api-key": KEY},
    json={"proxy": os.environ["PROXY_URL"], "headless": True},
    timeout=120,
)
res.raise_for_status()
ws_url = res.json()["wsUrl"]


def survey(browser, page):
    """One load in a fresh context: DOM ready + 6 s, then the main response's headers, the cookies, every URL."""
    context = browser.new_context()
    tab = context.new_page()
    urls = []
    tab.on("request", lambda r: urls.append(r.url))
    h, status = {}, 0
    try:
        r = tab.goto(page["url"], wait_until="domcontentloaded", timeout=45000)
        status = r.status if r else 0
        h = r.all_headers() if r else {}
        tab.wait_for_timeout(6000)
        title = tab.title()
        try:
            text = re.sub(r"\s+", " ", tab.locator("body").inner_text())
        except Exception:
            text = ""
        frames = " ".join(f.url for f in tab.frames)
        if CHALLENGE.search(f"{title} {text[:800]}") or "captcha-delivery.com" in frames:
            outcome = "challenge"
        elif BLOCKED.search(f"{title} {text[:400]}") or (status in (403, 429, 444) and len(text) < 1500):
            outcome = "blocked"
        else:
            outcome = "content"
    except Exception:
        outcome = "error"
    cookies = [c["name"] for c in context.cookies()]
    context.close()
    hits = [(v, label) for v, label, test in SIGNALS if test(h, cookies, urls)]
    found = list(dict.fromkeys(v for v, _ in hits))
    return {"status": status, "outcome": outcome, "vendors": found, "signals": hits}


with sync_playwright() as p:
    browser = p.chromium.connect(ws_url, headers={"x-api-key": KEY})
    try:
        vendors = {v: {"vendor": v, "pages": 0, "signals": {}, "outcomes": {"content": 0, "challenge": 0, "blocked": 0, "error": 0}} for v in VENDORS}
        demo_pages, none, expected_matched = [], 0, 0
        for page in PAGES:
            r = survey(browser, page)
            if not r["vendors"]:
                none += 1
            if page["expect"] in r["vendors"]:
                expected_matched += 1
            for v in r["vendors"]:
                vendors[v]["pages"] += 1
                vendors[v]["outcomes"][r["outcome"]] += 1
            for v, label in r["signals"]:
                vendors[v]["signals"][label] = vendors[v]["signals"].get(label, 0) + 1
            if page.get("demo"):
                demo_pages.append({"page": page["demo"], "vendors": r["vendors"], "outcome": r["outcome"]})
        print(json.dumps({
            "measured_at": datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%d"),
            "pages": len(PAGES),
            "no_vendor_found": none,
            "expected_vendor_found": expected_matched,
            "vendors": [vendors[v] for v in VENDORS if vendors[v]["pages"] > 0],
            "demo_pages": demo_pages,
        }, indent=2))
    finally:
        browser.close()
