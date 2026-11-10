# pip install requests
# env: CDPFLEET_API_KEY (a paid plan: jobs aren't on Community)
import hashlib
import hmac
import json
import math
import os
import re
import secrets
import threading
import time
from datetime import datetime, timezone

import requests

KEY = os.environ["CDPFLEET_API_KEY"]
API = "https://cdpfleet.com"
# A public page that changes on its own: it asks its server for the ISS position about once a second
# (XHR to /w/ajax/realtime) and rewrites a small table with it.
PAGE = "https://wheretheiss.at/"
WATCH = {
    "url": PAGE,
    "browser": "chrome",
    "duration": 300,  # a safety net: the program stops the job itself
    "filter": {"url": ["*/w/ajax/realtime*"], "selector": ".span3 table.table-striped", "types": ["xhr", "fetch", "dom"]},
}
STOP_AFTER_S = 40  # DELETE the job this long after starting it
# A second, short watch for WebSocket messages: mempool.space pushes Bitcoin mempool stats to its page over one.
# No browser given: watch defaults to Chrome (WebSocket capture needs a Chromium-based browser).
WS_WATCH = {"url": "https://mempool.space/", "duration": 120, "filter": {"url": ["*/api/v1/ws*"], "types": ["ws"]}}
WS_STOP_AFTER_S = 45
ENDED = ["done", "failed", "stopped", "interrupted"]
HEADERS = {"x-api-key": KEY}
UUID = re.compile(r"^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$")


def api(method, path, body=None):
    return requests.request(method, API + path, headers=HEADERS, json=body, timeout=90)


def ms(iso):  # an API timestamp as epoch milliseconds
    return datetime.fromisoformat(iso.replace("Z", "+00:00")).timestamp() * 1000


def now_ms():
    return time.time() * 1000


def secs(m):
    return round(m / 1000, 1)


# x-cdpfleet-signature: t=<unix>,v1=<hex HMAC-SHA256(secret, "<t>.<raw body>")>, checked on the raw bytes.
def verify(secret, header, raw_body):
    parts = dict(kv.split("=", 1) for kv in (header or "").split(",") if "=" in kv)
    if "t" not in parts or "v1" not in parts:
        return False
    expected = hmac.new(secret.encode(), f"{parts['t']}.{raw_body}".encode(), hashlib.sha256).hexdigest()
    return hmac.compare_digest(expected, parts["v1"])


def km(a, b):  # great-circle distance in km
    r = math.radians
    h = math.sin(r(b["lat"] - a["lat"]) / 2) ** 2 + math.cos(r(a["lat"])) * math.cos(r(b["lat"])) * math.sin(r(b["lon"] - a["lon"]) / 2) ** 2
    return 2 * 6371 * math.asin(math.sqrt(h))


def by_type(events):
    return {t: n for t in ["xhr", "fetch", "ws", "dom"] if (n := sum(1 for e in events if e["type"] == t))}


def key(e):
    return f"{e['type']} {e['at']} {e.get('url') or e.get('selector')}"


# The webhook receiver here is cdpfleet.com's own test inbox: it keeps deliveries an hour (raw body and
# headers) and reads them back with GET. Use your own public https endpoint.
hook = f"{API}/e2e/hook/{secrets.token_urlsafe(18)}"
secret = api("GET", "/v1/me/webhook-secret").json()["secret"]

# 1. Start the watch: XHR/fetch calls matching the glob, and changes under the selector, with a webhook.
t0 = now_ms()
start = api("POST", "/v1/functions/watch", {**WATCH, "deliver": {"webhook": {"url": hook, "events": ["event", "done"]}}})
if start.status_code != 202:
    raise SystemExit(f"watch {start.status_code} {start.text}")
job_id = start.json()["job_id"]

# 2. Follow it live on the SSE stream; stop it with DELETE after STOP_AFTER_S seconds.
stop = {}


def do_stop():
    t = now_ms()
    r = api("DELETE", f"/v1/jobs/{job_id}")
    stop.update(at=t, status=r.status_code, reply=r.json().get("status"))


timer = threading.Timer(STOP_AFTER_S, do_stop)
timer.start()

live = []  # every event as it arrived: (event, received)
seen = {"progress": 0, "part": 0, "event": 0, "done": 0}
done_data = None
with requests.get(f"{API}/v1/jobs/{job_id}/events", headers={**HEADERS, "accept": "text/event-stream"}, stream=True, timeout=(30, 240)) as sse:
    name, data = None, []
    for line in sse.iter_lines(decode_unicode=True):
        if line:
            if line.startswith("event:"):
                name = line[6:].strip()
            elif line.startswith("data:"):
                data.append(line[5:].strip())
            continue
        if name:  # a blank line ends a message; a keep-alive comment has no event name
            seen[name] = seen.get(name, 0) + 1
            payload = json.loads("\n".join(data)) if data else {}
            if name == "event":
                live += [(ev, now_ms()) for ev in payload["events"]]
            if name == "done":
                done_data = payload
                break
        name, data = None, []
timer.cancel()
t_end = now_ms()

# 3. The job as it ended, and its events as stored.
job = api("GET", f"/v1/jobs/{job_id}").json()
for _ in range(20):
    if job["status"] in ENDED:
        break
    time.sleep(2)
    job = api("GET", f"/v1/jobs/{job_id}").json()
stored, cursor = [], 0
while cursor is not None:
    r = api("GET", f"/v1/jobs/{job_id}/results?cursor={cursor}").json()
    stored += r["results"]
    if r["next_cursor"] == cursor:
        break
    cursor = r["next_cursor"]

# 4. The webhook deliveries: job.event batches (at most one per 2 s) and job.done. Wait for job.done.
items = []
deadline = time.time() + 90
while time.time() < deadline:
    items = requests.get(hook, timeout=30).json()["items"]
    if any(d["event"] == "job.done" for d in items):
        break
    time.sleep(2)
hooks = [{**d, "json": json.loads(d["body"])} for d in items]
batches = [d for d in hooks if d["event"] == "job.event"]
done_hook = next((d for d in hooks if d["event"] == "job.done"), None)
hook_events = [e for d in batches for e in d["json"]["data"]["events"]]
sent = sorted(ms(d["json"]["created_at"]) for d in batches)
gaps = [b - a for a, b in zip(sent, sent[1:])]
# How long after its newest event each batch was signed (the signature's t, whole seconds).
delays = sorted(int(re.search(r"t=(\d+)", d["signature"]).group(1)) - max(ms(e["at"]) for e in d["json"]["data"]["events"]) / 1000 for d in batches)
stored_keys = {key(e) for e in stored}
hook_keys = {key(e) for e in hook_events}

# 5. What the events say: the ISS position from each XHR response, the table text from each DOM change.
positions = []
for e in stored:
    if e["type"] == "xhr" and e.get("response_body"):  # an XHR still in flight when the page reloads has no body
        d = json.loads(e["response_body"])["data"]
        positions.append({"at": ms(e["at"]), "lat": d["lat"], "lon": d["lon"], "time": d["time"]})
first = positions[0] if positions else None
last = positions[-1] if positions else None
lags = sorted((rec - ms(ev["at"])) / 1000 for ev, rec in live)
dom = [e for e in stored if e["type"] == "dom"]


def first_of(t):
    return next((secs(rec - t0) for ev, rec in live if ev["type"] == t), None)


# 6. WebSocket messages: start the second watch, stop it after WS_STOP_AFTER_S, read what it stored.
t_ws = now_ms()
ws_start = api("POST", "/v1/functions/watch", WS_WATCH)
if ws_start.status_code != 202:
    raise RuntimeError(f"ws watch {ws_start.status_code} {ws_start.text}")
ws_id = ws_start.json()["job_id"]
time.sleep(max(0.0, WS_STOP_AFTER_S - (now_ms() - t_ws) / 1000))
api("DELETE", f"/v1/jobs/{ws_id}")
ws_job = api("GET", f"/v1/jobs/{ws_id}").json()
for _ in range(20):
    if ws_job["status"] in ENDED:
        break
    time.sleep(2)
    ws_job = api("GET", f"/v1/jobs/{ws_id}").json()
ws_events, cursor = [], 0
while cursor is not None:
    r = api("GET", f"/v1/jobs/{ws_id}/results?cursor={cursor}").json()
    ws_events += r["results"]
    if r["next_cursor"] == cursor:
        break
    cursor = r["next_cursor"]


def parse(text):  # each message is the frame's text: here JSON, cut at 64 KB (a cut one no longer parses)
    try:
        return json.loads(text)
    except ValueError:
        return None


ws_in = [e for e in ws_events if e["type"] == "ws" and e["direction"] == "in"]
ws_out = [e for e in ws_events if e["type"] == "ws" and e["direction"] == "out"]
in_keys = {}
for e in ws_in:
    msg = parse(e["data"])
    k = next(iter(msg), "(cut at 64 KB)") if isinstance(msg, dict) else "(cut at 64 KB)"
    in_keys[k] = in_keys.get(k, 0) + 1
mempool = [m["mempoolInfo"] for m in map(lambda e: parse(e["data"]), ws_in) if isinstance(m, dict) and m.get("mempoolInfo")]

# 7. The bill: a watch runs in your own browser session, one thread for a headless Chrome.
since = datetime.fromtimestamp(t0 / 1000 - 60, timezone.utc).isoformat().replace("+00:00", "Z")
sessions = requests.get(f"{API}/v1/me/sessions", headers=HEADERS, params={"from": since, "limit": 200}, timeout=60).json().get("sessions", [])
session = next((s for s in sessions if job.get("session_id") in (s.get("session_id"), s.get("id"))), None)
ws_session = next((s for s in sessions if ws_job.get("session_id") in (s.get("session_id"), s.get("id"))), None)

print(json.dumps({
    "language": "python",
    "page": PAGE,
    "watch": {**WATCH, "stop_after_s": STOP_AFTER_S, "start_status": start.status_code},
    "sse": {
        "events": len(live), "by_type": by_type([ev for ev, _ in live]), "messages": seen,
        "first_event_seconds": secs(live[0][1] - t0) if live else None,
        "first_xhr_seconds": first_of("xhr"), "first_dom_seconds": first_of("dom"),
        "lag_seconds": {"min": round(lags[0], 2), "median": round(lags[len(lags) // 2], 2), "max": round(lags[-1], 2)} if lags else None,
        "done_status": (done_data or {}).get("status"), "stream_seconds": secs(t_end - t0),
    },
    "stop": {"after_seconds": secs(stop["at"] - t0), "status": stop["status"], "reply": stop["reply"], "seconds_to_ended": secs(ms(job["ended_at"]) - stop["at"])} if stop else None,
    "job": {
        "status": job["status"], "own_session": job.get("own_session"), "concurrency": job.get("concurrency"), "runs": job.get("runs"),
        "progress": job.get("progress"), "billed_seconds": job.get("billed_seconds"),
        "run_seconds": secs(ms(job["ended_at"]) - ms(job["started_at"])),
        "results": {"parts": job["results"]["parts"], "bytes": job["results"]["bytes"]} if job.get("results") else None, "error": job.get("error"),
    },
    "session": {"engine": session["engine"], "weight": session["weight"], "seconds": secs(ms(session["ended_at"]) - ms(session["started_at"]))} if session else None,
    "stored": {
        "events": len(stored), "by_type": by_type(stored),
        "same_as_live": len(stored) == len(live) and all(key(ev) in stored_keys for ev, _ in live),
        "xhr_urls_match_glob": all("/w/ajax/realtime" in e["url"] and e["status"] == 200 and e["method"] == "GET" for e in stored if e["type"] == "xhr"),
        "xhr_without_body": sum(1 for e in stored if e["type"] == "xhr" and not e.get("response_body")),
        "positions": len(positions), "distinct_positions": len({(p["lat"], p["lon"]) for p in positions}),
        "first": {"lat": round(first["lat"], 3), "lon": round(first["lon"], 3), "time": first["time"]} if first else None,
        "last": {"lat": round(last["lat"], 3), "lon": round(last["lon"], 3), "time": last["time"]} if last else None,
        "ground_km": round(km(first, last), 1) if first else None,
        "ground_km_per_s": round(km(first, last) / ((last["at"] - first["at"]) / 1000), 2) if first and last["at"] > first["at"] else None,
        "dom_events": len(dom), "dom_text_sample": " ".join(dom[0]["text"].split()) if dom else None,
        "dom_html_bytes": len(dom[0]["html"].encode()) if dom else None,
    },
    "webhook": {
        "deliveries": len(hooks), "inbox_full": len(items) >= 20,
        "by_event": {e: sum(1 for d in hooks if d["event"] == e) for e in dict.fromkeys(d["event"] for d in hooks)},
        "signatures_valid": sum(1 for d in hooks if verify(secret, d["signature"], d["body"])),
        "tampered_body_rejected": bool(hooks) and not verify(secret, hooks[0]["signature"], hooks[0]["body"] + " "),
        "delivery_ids_unique_uuids": len({d["delivery"] for d in hooks}) == len(hooks) and all(UUID.match(d["delivery"] or "") for d in hooks),
        "job_id_matches": all(d["json"].get("job_id") == job_id for d in hooks),
        "events": len(hook_events), "events_by_type": by_type(hook_events), "events_all_stored": all(key(e) in stored_keys for e in hook_events),
        "stored_not_in_webhooks": sum(1 for e in stored if key(e) not in hook_keys),
        "not_sent_are_the_last": all(ms(e["at"]) >= max((ms(h["at"]) for h in hook_events), default=0) for e in stored if key(e) not in hook_keys),
        "signed_after_newest_event_seconds": {"min": round(delays[0], 1), "max": round(delays[-1], 1)} if delays else None,
        "min_gap_seconds": secs(min(gaps)) if gaps else None,
        "min_gap_before_stop_seconds": secs(min(gaps[:-1])) if len(gaps) > 1 else None,
        "max_events_per_batch": max([len(d["json"]["data"]["events"]) for d in batches], default=0),
        "done": {"status": done_hook["json"]["data"].get("status"), "progress": done_hook["json"]["data"].get("progress"), "error": done_hook["json"]["data"].get("error")} if done_hook else None,
    },
    "ws": {
        "page": WS_WATCH["url"], "filter": WS_WATCH["filter"], "start_status": ws_start.status_code, "status": ws_job["status"],
        "session_engine": ws_session["engine"] if ws_session else None,
        "events": len(ws_events), "in": len(ws_in), "out": len(ws_out), "urls": list(dict.fromkeys(e["url"] for e in ws_events)),
        "first_in_seconds": secs(ms(ws_in[0]["at"]) - t_ws) if ws_in else None,
        "sent": [e["data"] for e in ws_out], "received": in_keys,
        "mempool_txs": {"first": mempool[0]["size"], "last": mempool[-1]["size"], "updates": len(mempool)} if mempool else None,
        "billed_seconds": ws_job.get("billed_seconds"),
    },
    "measured_at": datetime.now(timezone.utc).strftime("%Y-%m-%d"),
}, indent=2, ensure_ascii=False))
