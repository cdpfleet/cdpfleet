# pip install requests
# env: CDPFLEET_API_KEY (five countries in one call hold five threads: a paid plan)
import json
import os
import re
import time
from datetime import datetime, timedelta, timezone

import requests

KEY = os.environ["CDPFLEET_API_KEY"]
API = "https://cdpfleet.com"
COUNTRIES = ["us", "de", "gb", "jp", "br"]  # the first one is the baseline the diff compares with
FORMATS = ["markdown", "html"]
MAX_WAIT = 45
PAGES = [
    ("Steam store page", "https://store.steampowered.com/app/620/Portal_2/"),
    ("Google home page", "https://www.google.com/"),
    ("Nike home page", "https://www.nike.com/"),
    ("example.com (control)", "https://example.com/"),
]
# Prices in the five countries' currencies: R$ before $, the symbol before or after the number.
# Plain [0-9] and [ \u00a0] rather than \d and \s, so the pattern means the same in all five languages.
PRICE = re.compile(
    "R\\$[ \u00a0]?[0-9][0-9.,]*|US\\$[ \u00a0]?[0-9][0-9.,]*|£[ \u00a0]?[0-9][0-9.,]*|€[ \u00a0]?[0-9][0-9.,]*"
    "|[0-9][0-9.,]*[ \u00a0]?€|[¥￥][ \u00a0]?[0-9][0-9,]*|[0-9][0-9,]*[ \u00a0]?円|\\$[ \u00a0]?[0-9][0-9.,]*"
)
# The "accept all" button of a cookie banner, in the five countries' languages.
CONSENT = re.compile("Accept all|Alle akzeptieren|Aceitar todos|Aceitar tudo|すべて同意|すべて受け入れる|すべて許可", re.I)
SINCE = (datetime.now(timezone.utc) - timedelta(seconds=60)).isoformat().replace("+00:00", "Z")
HEADERS = {"x-api-key": KEY}


def nbytes(s):
    return len(s.encode()) if isinstance(s, str) else 0


def prices(text):
    return [re.sub("[.,]$", "", p).replace("\u00a0", " ", 1) for p in PRICE.findall(text)]


def currency(p):
    if p.startswith("R$"):
        return "BRL"
    if "£" in p:
        return "GBP"
    if "€" in p:
        return "EUR"
    if re.search("[¥￥円]", p):
        return "JPY"
    return "USD"


def attr(html, pattern):
    m = re.search(pattern, html, re.I)
    return m.group(1) if m else None


def read_diff(d):  # what the Function's unified diff (one country against the baseline) says
    if not isinstance(d, str):
        return None
    lines = d.split("\n")
    added = [l for l in lines if l.startswith("+") and not l.startswith("+++")]
    removed = [l for l in lines if l.startswith("-") and not l.startswith("---")]
    was = [p for l in removed for p in prices(l)]
    now = [p for l in added for p in prices(l)]
    changes = []
    for a, b in zip(was, now):
        if len(changes) == 3:
            break
        if a != b:
            changes.append(f"{a} → {b}")
    return {"bytes": nbytes(d), "hunks": sum(l.startswith("@@") for l in lines), "added": len(added),
            "removed": len(removed), "price_changes": changes}


def billed(call_id):
    # The call in the account's history (its x-call-id is the session id there): weight and billed thread-seconds.
    for _ in range(30 if call_id else 0):
        r = requests.get(f"{API}/v1/me/sessions", params={"from": SINCE, "limit": 200}, headers=HEADERS, timeout=30)
        s = next((x for x in r.json().get("sessions", []) if x["session_id"] == call_id), None)
        if s and s.get("ended_at"):
            t = lambda v: datetime.fromisoformat(v.replace("Z", "+00:00")).timestamp()
            return {"engine": s["engine"], "weight": s["weight"],
                    "thread_seconds": round((t(s["ended_at"]) - t(s["started_at"])) * s["weight"], 1)}
        time.sleep(2)
    return None


def geo(url, countries=COUNTRIES, max_wait=MAX_WAIT, formats=FORMATS):
    t = time.time()
    res = requests.post(f"{API}/v1/functions/geo", headers=HEADERS, timeout=max_wait + 30,
                        json={"url": url, "countries": countries, "formats": formats, "max_wait": max_wait})
    try:
        body = res.json()
    except ValueError:
        body = {}
    return {"res": res, "body": body if isinstance(body, dict) else {}, "ms": (time.time() - t) * 1000}


def loaded(r):
    return isinstance(r, dict) and isinstance(r.get("status"), int) and 200 <= r["status"] < 400


pages = []
for name, url in PAGES:  # one call per page, one after another
    call = first = geo(url)
    # Every diff is against the first country: if that one failed, the call is retried once.
    if call["res"].status_code == 200 and not loaded(call["body"].get("results", {}).get(COUNTRIES[0])):
        call = geo(url)
    res, body = call["res"], call["body"]
    results = body.get("results") or {}
    diff = body.get("diff") or {}
    base = results.get(COUNTRIES[0]) or {}
    countries = {}
    for c in COUNTRIES:
        r = results.get(c) or {}
        html = r.get("html") or ""
        md = r.get("markdown") or ""
        found = prices(md)
        title = attr(html, r"<title[^>]*>([^<]*)<")
        countries[c] = {
            "status": r.get("status"),
            "error": r.get("error"),
            "partial": r.get("partial") is True,
            "lang": attr(html, r'<html[^>]*\slang="([^"]+)"'),
            "url": r.get("url"),  # the final URL after redirects (only for a country that loaded)
            "title": title.strip() if title is not None else None,
            "currencies": list(dict.fromkeys(currency(p) for p in found)),
            "prices": found[:4],
            "consent_banner": bool(CONSENT.search(html)) if html else None,
            "markdown_bytes": nbytes(md),
            "html_bytes": nbytes(html),
            "same_markdown_as_baseline": None if c == COUNTRIES[0] or not md or not base.get("markdown") else md == base["markdown"],
            # A diff is a string ("" = identical), or None (null) when this country or the baseline failed.
            "diff": None if c == COUNTRIES[0] else {
                "markdown": read_diff((diff.get("markdown") or {}).get(c)),
                "html_bytes": None if (diff.get("html") or {}).get(c) is None else nbytes(diff["html"][c]),
            },
        }
    fresults = first["body"].get("results") or {}
    pages.append({
        "page": name,
        "url": url,
        "attempts": 1 if call is first else 2,
        "first_attempt": None if call is first else {
            "call_id": first["res"].headers.get("x-call-id"),
            "errors": {c: (fresults.get(c) or {}).get("error") for c in COUNTRIES if not loaded(fresults.get(c))},
            "diffs": {c: None if d is None else nbytes(d) for c, d in ((first["body"].get("diff") or {}).get("markdown") or {}).items()},
        },
        "status": res.status_code,
        "error": body.get("error"),
        "seconds": round(call["ms"] / 1000, 1),
        "call_id": res.headers.get("x-call-id"),
        "warning": res.headers.get("x-cdpfleet-warning"),
        "response_bytes": len(res.content),
        "diff_bytes": {f: sum(nbytes(d) for d in (diff.get(f) or {}).values()) for f in ("markdown", "html")},
        "ok_countries": [c for c in COUNTRIES if loaded(results.get(c))],
        "countries": countries,
    })

# What a failure looks like: the same Steam page with max_wait 1, so no country can load it in time.
failed = geo("https://store.steampowered.com/app/620/Portal_2/", ["us", "de"], 1, ["markdown"])
failure = {
    "status": failed["res"].status_code, "max_wait": 1,
    "results": {c: {"status": r.get("status"), "error": r.get("error")} for c, r in (failed["body"].get("results") or {}).items()},
    "diff": (failed["body"].get("diff") or {}).get("markdown"),
}

for p in pages:
    p["billed"] = billed(p["call_id"])
    if p["first_attempt"]:
        p["first_attempt"]["billed"] = billed(p["first_attempt"]["call_id"])

print(json.dumps({"language": "python", "countries": COUNTRIES, "formats": FORMATS, "max_wait": MAX_WAIT, "pages": pages, "failure": failure,
                  "measured_at": datetime.now(timezone.utc).strftime("%Y-%m-%d")}, indent=2, ensure_ascii=False))
