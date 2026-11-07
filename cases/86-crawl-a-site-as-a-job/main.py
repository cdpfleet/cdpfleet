# pip install requests
# env: CDPFLEET_API_KEY (a paid plan: jobs aren't on Community)
import hashlib
import hmac
import io
import json
import os
import re
import secrets
import time
import zipfile
from datetime import datetime, timezone

import requests

KEY = os.environ["CDPFLEET_API_KEY"]
API = "https://cdpfleet.com"
SITE = "https://books.toscrape.com/"  # a sandbox made for scraping practice
CRAWL = {"max_depth": 2, "max_pages": 100, "concurrency": 4, "delay_ms": 250, "formats": ["markdown"]}
EXTRACT = {"title": "h1", "price": ".product_main .price_color"}
STOP_AT = 30  # stop the job once this many pages are done, then resume it
ENDED = ["done", "failed", "stopped", "interrupted"]
HEADERS = {"x-api-key": KEY}


def secs(s):
    return round(s, 1)


def api(method, path, body=None):
    return requests.request(method, API + path, headers=HEADERS, json=body, timeout=90)


def wait_for(job_id, until, seconds):
    end = time.time() + seconds
    job = None
    while time.time() < end:
        job = api("GET", f"/v1/jobs/{job_id}").json()
        if until(job):
            return job
        time.sleep(3)
    raise RuntimeError(f"job {job_id}: still {job and job.get('status')} after {seconds} s")


def stored(job_id):  # results stored so far, part by part
    rows, cursor = [], 0
    while cursor is not None:
        r = api("GET", f"/v1/jobs/{job_id}/results?cursor={cursor}").json()
        rows += r["results"]
        if r["next_cursor"] == cursor:  # nothing new yet
            break
        cursor = r["next_cursor"]
    return rows


def hook_items(hook):
    return requests.get(hook, timeout=30).json()["items"]


# 1. Map: the site's URLs in one instant call (sitemaps + the start page's links).
t = time.time()
m = api("POST", "/v1/functions/map", {"url": SITE, "max_urls": 5000})
mj = m.json() if m.ok else {}
map_out = {
    "status": m.status_code, "seconds": secs(time.time() - t), "urls": len(mj.get("urls", [])) if m.ok else None,
    "sources": mj.get("sources"), "partial": m.headers.get("x-cdpfleet-partial"),
    "categories": sum("/catalogue/category/" in u for u in mj.get("urls", [])),
}

# 2. Start the crawl as a job: a webhook when it's done, and a zip archive at the end.
# The receiver here is cdpfleet.com's own test inbox (keeps deliveries an hour, read back with GET);
# use your own public https endpoint.
hook = f"{API}/e2e/hook/{secrets.token_urlsafe(18)}"
t_start = time.time()
start = api("POST", "/v1/functions/crawl", {
    "url": SITE, **CRAWL, "extract": EXTRACT,
    "deliver": {"webhook": {"url": hook, "events": ["done"]}, "format": "archive"},
})
if start.status_code != 202:
    raise RuntimeError(f"crawl {start.status_code} {start.text}")
job_id = start.json()["job_id"]

# 3. Watch it, and stop it part-way.
before = wait_for(job_id, lambda j: (j.get("progress") or {}).get("done", 0) >= STOP_AT or j["status"] in ENDED, 600)
stop_reply = api("DELETE", f"/v1/jobs/{job_id}")
t_stop = time.time()
stopped = wait_for(job_id, lambda j: j["status"] in ENDED, 300)
t_stopped = time.time()
at_stop = stored(job_id)
hooks_at_stop = hook_items(hook)
archive_at_stop = api("GET", f"/v1/jobs/{job_id}/archive").json()

# 4. Resume: a new run of the same job, from its checkpoint.
t_resume = time.time()
resume = api("POST", f"/v1/jobs/{job_id}/resume")
# 5. Wait for the second run to end.
done = wait_for(job_id, lambda j: j.get("runs", 0) >= 2 and j["status"] in ENDED, 900)
t_done = time.time()

# 6a. Every result as one JSON-lines file.
jsonl = api("GET", f"/v1/jobs/{job_id}/results.jsonl").content.decode("utf-8")
rows = [json.loads(line) for line in jsonl.strip().split("\n")]
urls = {r["url"] for r in rows}
first_run = {r["url"] for r in at_stop}

# 6b. The same results as a zip (built when the job ended).
wait_for(job_id, lambda j: (j.get("archive") or {}).get("status") in ("ready", "failed"), 300)
archive = api("GET", f"/v1/jobs/{job_id}/archive").json()
zip_bytes = requests.get(archive["url"], timeout=120).content  # a storage link, no key
with zipfile.ZipFile(io.BytesIO(zip_bytes)) as z:
    names = z.namelist()
    index = z.read("index.jsonl").decode("utf-8") if "index.jsonl" in names else ""


# 7. The webhooks: every delivery is signed with the account's secret, as HMAC-SHA256(secret, "<t>.<raw body>").
secret = api("GET", "/v1/me/webhook-secret").json()["secret"]


def check(d):
    parts = dict(kv.split("=", 1) for kv in (d.get("signature") or "").split(",") if "=" in kv)
    body = json.loads(d["body"])
    want = hmac.new(secret.encode(), f"{parts.get('t')}.{d['body']}".encode(), hashlib.sha256).hexdigest()
    return {
        "event": d["event"], "status": (body.get("data") or {}).get("status"), "job_id_matches": body.get("job_id") == job_id,
        "delivery_id_is_uuid": bool(re.fullmatch(r"[0-9a-f-]{36}", d.get("delivery") or "")),
        "signature_valid": bool(parts.get("v1")) and hmac.compare_digest(parts["v1"], want),
        "signed_at": int(parts.get("t") or 0), "data": body.get("data"),
    }


deliveries = []
end = time.time() + 90
while time.time() < end:
    deliveries = [check(d) for d in hook_items(hook)]
    if any(d["event"] == "job.done" and d["status"] == "done" for d in deliveries):
        break
    time.sleep(3)
final = next((d for d in deliveries if d["event"] == "job.done" and d["status"] == "done"), None)

# 8. What it cost: a run holds `concurrency` threads, billed_seconds is the total in thread-seconds.
run_seconds = done["billed_seconds"] / done["concurrency"]
ended_at = datetime.fromisoformat(done["ended_at"].replace("Z", "+00:00")).timestamp()
depths = sorted({r.get("depth") for r in rows})

print(json.dumps({
    "language": "python",
    "site": SITE,
    "map": map_out,
    "crawl": {**CRAWL, "extract": list(EXTRACT), "start_status": start.status_code},
    "stop": {
        "requested_at_done": (before.get("progress") or {}).get("done", 0),
        "reply": (stop_reply.json() or {}).get("status", stop_reply.status_code),
        "seconds_to_stopped": secs(t_stopped - t_stop), "status": stopped["status"], "progress": stopped.get("progress"),
        "stored_results": len(at_stop),
        "webhooks_by_then": [f"{d['event']} {(json.loads(d['body']).get('data') or {}).get('status')}" for d in hooks_at_stop],
        "archive": archive_at_stop.get("status"),
    },
    "resume": {"status": resume.status_code, "reply": (resume.json() or {}).get("status")},
    "job": {
        "status": done["status"], "runs": done["runs"], "progress": done.get("progress"), "concurrency": done["concurrency"],
        "billed_seconds": done["billed_seconds"],
        "results": {"parts": done["results"]["parts"], "bytes": done["results"]["bytes"]},
        "started_at": done["started_at"], "ended_at": done["ended_at"], "expires_at": done["expires_at"],
    },
    "results_jsonl": {
        "lines": len(rows), "distinct_urls": len(urls), "bytes": len(jsonl.encode("utf-8")),
        "status_200": sum(r.get("status") == 200 for r in rows),
        "errors": [{"url": r["url"], "status": r.get("status"), "error": str(r["error"])[:120]} for r in rows if r.get("error")],
        "with_markdown": sum(isinstance(r.get("markdown"), str) and len(r["markdown"]) > 0 for r in rows),
        "books_with_price": sum(isinstance((r.get("extract") or {}).get("price"), str) for r in rows),
        "category_pages": sum("/catalogue/category/" in r["url"] for r in rows),
        "depths": {str(d): sum(r.get("depth") == d for r in rows) for d in depths},
        "first_run": len(first_run), "second_run_new": len(urls - first_run),
        "sample": [{"url": r["url"], "title": r["extract"]["title"], "price": r["extract"]["price"]}
                   for r in rows if (r.get("extract") or {}).get("price")][:2],
    },
    "archive": {
        "status": archive["status"], "bytes": archive["bytes"], "downloaded_bytes": len(zip_bytes), "files": len(names),
        "markdown_files": sum(n.endswith("/index.md") for n in names),
        "extract_files": sum(n.endswith("/extract.json") for n in names),
        "index_lines": len(index.strip().split("\n")) if index.strip() else 0,
        "has": [n for n in ["index.jsonl", "errors.jsonl", "job.json"] if n in names],
    },
    "webhook": {
        "deliveries": [{k: v for k, v in d.items() if k not in ("data", "signed_at")} for d in deliveries],
        "final_done": {"data": final["data"], "signed_seconds_after_end": final["signed_at"] - int(ended_at)} if final else None,
    },
    "cost": {
        "billed_thread_seconds": done["billed_seconds"], "pages": len(rows),
        "run_seconds": round(run_seconds, 1),
        "pages_per_minute": round(len(rows) / (run_seconds / 60), 1),
        "thread_seconds_per_page": round(done["billed_seconds"] / len(rows), 1),
        "client_running_seconds": secs(t_stopped - t_start + t_done - t_resume),
    },
    "measured_at": datetime.now(timezone.utc).date().isoformat(),
}, indent=2, ensure_ascii=False))
