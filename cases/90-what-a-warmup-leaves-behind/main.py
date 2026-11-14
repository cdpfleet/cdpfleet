# pip install playwright==1.60.0 requests
# env: CDPFLEET_API_KEY (a paid plan: jobs and profiles aren't on Community), PROXY_URL_US (a sticky US exit)
import json
import os
import re
import secrets
import time
from datetime import datetime, timezone
from urllib.parse import quote, urlparse

import requests
from playwright.sync_api import sync_playwright

KEY = os.environ["CDPFLEET_API_KEY"]
PROXY = os.environ["PROXY_URL_US"]
API = "https://cdpfleet.com"
NAME = f"case90-python-{secrets.token_hex(4)}"  # a profile of its own for each run
# Three ordinary public sites: an encyclopedia, a scraping sandbox, a news front page.
SITES = ["https://en.wikipedia.org/wiki/Main_Page", "https://books.toscrape.com/", "https://www.theguardian.com/us"]
WARMUP = {
    "url": SITES[0],
    "sites": SITES[1:],
    "browser": "camoufox",
    "profile": NAME,
    "duration": 600,  # a safety net: the program stops the job itself
    # No typing: the warmup would otherwise type into search boxes now and then.
    "behavior": {"scroll": True, "mouse": True, "typing": False, "dwell_s": [4, 10], "pages_per_site": [2, 4]},
}
STOP_AFTER_S = 150
ENDED = ["done", "failed", "stopped", "interrupted"]
DAY = 86400
HEADERS = {"x-api-key": KEY}


def api(method, path, body=None):
    return requests.request(method, API + path, headers=HEADERS, json=body, timeout=90)


def ms(iso):  # an API timestamp as epoch milliseconds
    return datetime.fromisoformat(iso.replace("Z", "+00:00")).timestamp() * 1000


def now_ms():
    return time.time() * 1000


def secs(m):
    return round(m / 1000, 1)


def profile_info():
    return next((p for p in api("GET", "/v1/me/profiles").json().get("profiles", []) if p["name"] == NAME), None)


# The site a cookie or a host belongs to: its last two labels (three under co.uk and the like).
def site(host):
    labels = host.lstrip(".").split(".")
    n = 3 if re.match(r"^(co|com|org|net|ac|gov)\.[a-z]{2}$", ".".join(labels[-2:])) else 2
    return ".".join(labels[-n:])


def key(c):
    return f"{c['name']}|{c['domain']}|{c['path']}"


# Cookies by site, first- vs third-party (first = a site the warmup visited as a page), and how long they last.
def summarize(cookies, first_party, now):
    by_site = {}
    for c in cookies:
        by_site[site(c["domain"])] = by_site.get(site(c["domain"]), 0) + 1
    third = sorted(((s, n) for s, n in by_site.items() if s not in first_party), key=lambda x: (-x[1], x[0]))
    left = [(c["expires"] - now) / DAY for c in cookies if c["expires"] > 0]
    first = sum(1 for c in cookies if site(c["domain"]) in first_party)
    keys = [key(c) for c in cookies]
    return {
        "total": len(cookies),
        "sites": len(by_site),
        "first_party": first,
        "third_party": len(cookies) - first,
        "third_party_sites": len(third),
        "by_first_party_site": {s: by_site.get(s, 0) for s in first_party},
        "top_third_party": [f"{s} {n}" for s, n in third[:5]],
        "session": len(cookies) - len(left),
        "http_only": sum(1 for c in cookies if c["httpOnly"]),
        "same_site_none": sum(1 for c in cookies if c["sameSite"] == "None"),
        "expiry": {
            "under_1_day": sum(1 for d in left if d < 1),
            "under_30_days": sum(1 for d in left if 1 <= d < 30),
            "under_1_year": sum(1 for d in left if 30 <= d < 365),
            "a_year_or_more": sum(1 for d in left if d >= 365),
            "max_days": round(max(left)) if left else None,
        },
        "same_key_twice": len(keys) - len(set(keys)),  # e.g. Firefox's per-site (partitioned) copies
    }


def launch(pw, profile):
    body = {"proxy": PROXY, "headless": True}
    if profile:
        body["profile"] = profile
    for attempt in range(1, 6):
        res = requests.post("https://starter.cdpfleet.com/camoufox/session", headers=HEADERS, json=body, timeout=120)
        if res.ok:
            data = res.json()
            return data["sessionId"], pw.firefox.connect(data["wsUrl"], headers=HEADERS)
        if res.status_code != 503 or attempt == 5:
            raise RuntimeError(f"launch {res.status_code} {res.text}")
        time.sleep(int(res.headers.get("retry-after") or 5))


STORAGE_JS = "async () => ({ local_storage: localStorage.length, indexeddb: (await indexedDB.databases()).length })"


# What one later session sees: the cookies it starts with, then for each warmed site one request for its
# /robots.txt — the Cookie header that first request carries is what the site gets from this browser, and
# the plain-text page lets us read the origin's localStorage and IndexedDB with none of the site's scripts run.
def inspect(pw, profile):
    session_id, browser = launch(pw, profile)
    t = now_ms()
    try:
        # With a profile, its context is browser.contexts[0]; without one, a fresh context.
        context = browser.contexts[0] if profile else browser.new_context()
        page = context.pages[0] if context.pages else context.new_page()
        at_start = context.cookies()
        origins = []
        for url in SITES:
            u = urlparse(url)
            origin = f"{u.scheme}://{u.netloc}"
            res = None
            for attempt in range(1, 4):
                try:
                    res = page.goto(f"{origin}/robots.txt", timeout=60000)
                    break
                except Exception:
                    if attempt == 3:
                        raise
                    time.sleep(2)
            header = res.request.all_headers().get("cookie", "")
            names = [c.split("=", 1)[0] for c in header.split("; ")] if header else []
            storage = page.evaluate(STORAGE_JS)
            origins.append({"origin": origin, "status": res.status, "cookie_header_cookies": len(names), "cookie_header_names": names[:8], **storage})
            time.sleep(3)
        return {"session_id": session_id, "cookies_at_start": at_start, "origins": origins, "seconds": secs(now_ms() - t)}
    finally:
        browser.close()


out = {
    "measured_at": datetime.now(timezone.utc).strftime("%Y-%m-%d"),
    "sites": SITES,
    "warmup": {**WARMUP, "profile": "case90-python-…", "stop_after_s": STOP_AFTER_S},
}
job_id = None
try:
    # 1. Start the warmup into a new profile, on a sticky US exit.
    t0 = now_ms()
    start = api("POST", "/v1/functions/warmup", {**WARMUP, "launch": {"headless": True, "proxy": PROXY}})
    if start.status_code != 202:
        raise RuntimeError(f"warmup {start.status_code} {start.text}")
    job_id = start.json()["job_id"]
    out["warmup"]["start_status"] = start.status_code

    # 2. Follow its progress; stop it after STOP_AFTER_S seconds.
    samples = []
    stop = None
    while True:
        time.sleep(5)
        job = api("GET", f"/v1/jobs/{job_id}").json()
        samples.append({"s": round((now_ms() - t0) / 1000), **(job.get("progress") or {})})
        if job["status"] in ENDED:
            break
        if not stop and now_ms() - t0 >= STOP_AFTER_S * 1000:
            r = api("DELETE", f"/v1/jobs/{job_id}")
            stop = {"after_seconds": secs(now_ms() - t0), "status": r.status_code, "reply": r.json().get("status"), "at": now_ms()}
        if now_ms() - t0 > (STOP_AFTER_S + 120) * 1000:
            raise RuntimeError(f"job still {job['status']}")
    kept = [p for i, p in enumerate(samples) if "visits" in p and (i == len(samples) - 1 or p["visits"] != samples[i + 1].get("visits"))]
    out["job"] = {
        "status": job["status"], "own_session": job.get("own_session"), "runs": job.get("runs"), "progress": job.get("progress"), "error": job.get("error"),
        "billed_seconds": job.get("billed_seconds"), "run_seconds": secs(ms(job["ended_at"]) - ms(job["started_at"])),
        "stop": stop and {"after_seconds": stop["after_seconds"], "status": stop["status"], "reply": stop["reply"], "seconds_to_ended": secs(ms(job["ended_at"]) - stop["at"])},
        "progress_samples": [{"s": p["s"], "visits": p.get("visits"), "sites": p.get("sites"), "challenges": p.get("challenges")} for p in kept],
    }

    # 3. The visit history: the job's results log every page it browsed.
    lines = []
    cursor = 0
    while cursor is not None:
        r = api("GET", f"/v1/jobs/{job_id}/results?cursor={cursor}").json()
        lines += r["results"]
        if r["next_cursor"] == cursor:
            break
        cursor = r["next_cursor"]
    visits = [l for l in lines if l["type"] == "visit"]
    first_party = sorted({site(urlparse(v["url"]).hostname) for v in visits})
    out["visits"] = {
        "count": len(visits), "challenged": sum(1 for v in visits if v.get("challenged")),
        "by_site": {s: sum(1 for v in visits if site(urlparse(v["url"]).hostname) == s) for s in first_party},
        "statuses": sorted({v["status"] for v in visits}),
        "dwell_s": {"min": min(v["dwell_s"] for v in visits), "max": max(v["dwell_s"] for v in visits)} if visits else None,
        "paths": [urlparse(v["url"]).hostname + urlparse(v["url"]).path for v in visits],
        "cookie_lines": sum(1 for l in lines if l["type"] == "cookies"),
    }

    # 4. The cookies it left: GET /v1/jobs/{id}/cookies (after the end, the last set it delivered).
    ck = api("GET", f"/v1/jobs/{job_id}/cookies")
    ckj = ck.json()
    job_cookies = ckj.get("cookies") or []
    out["job_cookies"] = {"status": ck.status_code, "live": ckj.get("live"), "at_vs_end_seconds": secs(ms(ckj["at"]) - ms(job["ended_at"])),
                          **summarize(job_cookies, first_party, time.time())}

    # 5. The profile, saved after the job's browser exited.
    p = None
    for _ in range(60):
        p = profile_info()
        if p and p["version"] >= 1 and not p["in_use"]:
            break
        time.sleep(2)
    out["profile"] = {"engine": p["engine"], "version": p["version"], "bytes": p["bytes"], "saved_after_end_seconds": secs(ms(p["saved_at"]) - ms(job["ended_at"]))} if p else None

    with sync_playwright() as pw:
        # 6. A later session on the profile (read-only: it sees the profile and doesn't change it), then a fresh one.
        later = inspect(pw, {"id": NAME, "mode": "readonly"})
        keys = {key(c) for c in later["cookies_at_start"]}
        job_keys = {key(c) for c in job_cookies}
        session = [c for c in job_cookies if not c["expires"] > 0]
        out["later"] = {
            "cookies_at_start": summarize(later["cookies_at_start"], first_party, time.time()),
            "job_cookies_restored": len(job_keys & keys),
            "job_cookies_distinct": len(job_keys),
            "session_cookies_restored": sum(1 for c in session if key(c) in keys),
            "session_cookies_in_job": len(session),
            "not_in_job": len(keys - job_keys),
            "origins": later["origins"],
            "seconds": later["seconds"],
        }
        fresh = inspect(pw, None)
        out["fresh"] = {"cookies_at_start": len(fresh["cookies_at_start"]), "origins": fresh["origins"], "seconds": fresh["seconds"]}

    # 7. The bill: the warmup's own session and the two later sessions, from the session history.
    since = datetime.fromtimestamp((t0 - 60000) / 1000, timezone.utc).strftime("%Y-%m-%dT%H:%M:%S.000Z")
    wanted = [job.get("session_id"), later["session_id"], fresh["session_id"]]
    sessions = []
    for _ in range(15):
        sessions = api("GET", f"/v1/me/sessions?from={quote(since)}&limit=200").json().get("sessions") or []
        if all(any((x.get("session_id") or x.get("id")) == s and x.get("ended_at") for x in sessions) for s in wanted):
            break
        time.sleep(2)

    def bill(sid):
        s = next((x for x in sessions if (x.get("session_id") or x.get("id")) == sid), None)
        if not s:
            return None
        return {"engine": s["engine"], "weight": s["weight"], "seconds": secs(ms(s["ended_at"]) - ms(s["started_at"])) if s.get("ended_at") else None}

    out["bill"] = {"warmup": {"billed_seconds": job.get("billed_seconds"), "session": bill(job.get("session_id"))}, "later": bill(later["session_id"]), "fresh": bill(fresh["session_id"])}
finally:
    # Stop the job if something failed early, then delete the profile once nothing holds it.
    if job_id and "job" not in out:
        api("DELETE", f"/v1/jobs/{job_id}")
    for _ in range(60):
        info = profile_info()
        if not (info and info["in_use"]):
            break
        time.sleep(2)
    d = api("DELETE", f"/v1/me/profiles/{NAME}")
    out["cleanup"] = {"delete_status": d.status_code, "deleted": d.status_code == 200 and d.json().get("deleted") is True and profile_info() is None}
print(json.dumps(out, indent=2, ensure_ascii=False))
