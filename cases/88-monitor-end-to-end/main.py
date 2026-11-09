# pip install requests
# env: CDPFLEET_API_KEY (a paid plan: monitors aren't on Community)
import hashlib
import hmac
import json
import os
import re
import secrets
import time
from datetime import datetime, timezone

import requests

KEY = os.environ["CDPFLEET_API_KEY"]
API = "https://cdpfleet.com"
PAGE = "https://quotes.toscrape.com/random"  # a practice site: a different quote on every load
MAX_RUNS = 5  # checks to trigger at most while waiting for a change (the same quote can come up twice)
HEADERS = {"x-api-key": KEY}
UUID = re.compile(r"^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$")
since = datetime.fromtimestamp(time.time() - 60, timezone.utc).isoformat().replace("+00:00", "Z")


def api(method, path, body=None):
    return requests.request(method, API + path, headers=HEADERS, json=body, timeout=90)


def ms(iso):  # an API timestamp as epoch milliseconds
    return datetime.fromisoformat(iso.replace("Z", "+00:00")).timestamp() * 1000


def secs(m):
    return round(m / 1000, 1)


def now_ms():
    return time.time() * 1000


# x-cdpfleet-signature: t=<unix>,v1=<hex HMAC-SHA256(secret, "<t>.<raw body>")>. Check it against the raw
# bytes received, before parsing them, and refuse old timestamps (replays).
def verify(secret, header, raw_body):
    parts = dict(kv.split("=", 1) for kv in (header or "").split(",") if "=" in kv)
    if "t" not in parts or "v1" not in parts:
        return False
    expected = hmac.new(secret.encode(), f"{parts['t']}.{raw_body}".encode(), hashlib.sha256).hexdigest()
    return hmac.compare_digest(expected, parts["v1"]) and abs(time.time() - int(parts["t"])) < 300


# The webhook receiver here is cdpfleet.com's own test inbox: it keeps deliveries an hour, raw body and
# headers, and reads them back with GET. Use your own public https endpoint.
hook = f"{API}/e2e/hook/{secrets.token_urlsafe(18)}"


def inbox():
    return requests.get(hook, timeout=30).json()["items"]


secret = api("GET", "/v1/me/webhook-secret").json()["secret"]

# 1. Create the monitor: only the quote element, as text; the tags dropped (remove) and the "(about)" link
# label taken out of the text (ignore) before comparing.
# every: "1h" is its own schedule; the program doesn't wait for it and triggers checks with POST …/run.
spec = {"url": PAGE, "format": "text", "selector": ".quote", "remove": [".tags"], "ignore": [r"\(about\)"], "every": "1h",
        "deliver": {"webhook": {"url": hook}}}
created = api("POST", "/v1/monitors", spec)
if created.status_code != 201:
    raise RuntimeError(f"create {created.status_code} {created.text}")
c = created.json()
mid = c["monitor_id"]
out = {"language": "python", "page": PAGE}
try:
    listed = any(m["monitor_id"] == mid for m in api("GET", "/v1/monitors").json()["monitors"])
    out["monitor"] = {
        "create_status": created.status_code, "every": c["every"], "format": c["format"], "selector": c["selector"],
        "remove": c["remove"], "ignore": c["ignore"], "webhook": (c.get("deliver") or {}).get("webhook", {}).get("url") == hook,
        "active": c["active"], "listed": listed,
        "first_check_due_seconds": secs(ms(c["next_run_at"]) - ms(c["created_at"])),
    }

    # 2. Check now, and again until the page has changed. Poll the monitor and the inbox every 0.5 s.
    checks = []
    change = None
    for n in range(1, MAX_RUNS + 1):
        t_run = now_ms()
        run = api("POST", f"/v1/monitors/{mid}/run")
        m, t_status, item, t_hook = None, None, None, None
        end = time.time() + 120
        while time.time() < end:
            if not t_status:
                m = api("GET", f"/v1/monitors/{mid}").json()
                if m["checks"] >= n:
                    t_status = now_ms()
            if t_status and m["last_status"] != "changed":
                break
            if not item:
                item = next((d for d in inbox() if d["event"] == "monitor.change"), None)
                if item:
                    t_hook = now_ms()
            if t_status and item:
                break
            time.sleep(0.5)
        if not t_status:
            raise RuntimeError(f"check {n}: no result after 120 s")
        checks.append({
            "n": n, "run_status": run.status_code, "status": m["last_status"], "error": m["last_error"],
            "seconds_to_status": secs(t_status - t_run), "check_seconds": secs(ms(m["last_run_at"]) - t_run),
            "t_run": t_run, "last_run_at": m["last_run_at"],
        })
        if m["last_status"] == "changed":
            change = {"n": n, "t_run": t_run, "item": item, "t_hook": t_hook}
            break

    # 3. The webhook: verify it, then compare it with the change as the API keeps it.
    deliveries = inbox()
    d = change and change["item"]
    if d:
        body = json.loads(d["body"])
        data = body.get("data") or {}
        t = int(re.search(r"t=(\d+)", d["signature"] or "").group(1))
        full = api("GET", f"/v1/monitors/{mid}/changes/{data['change_id']}").json()
        before = requests.get(full["before_url"], timeout=60).content.decode() if full.get("before_url") else None
        after = requests.get(full["after_url"], timeout=60).content.decode()
        snapshot = requests.get(data["snapshot_url"], timeout=60).content.decode()
        lines = data["diff"].split("\n")
        out["webhook"] = {
            "deliveries": len(deliveries), "event_header": d["event"], "type": body.get("type"),
            "delivery_id_is_uuid": bool(UUID.match(d["delivery"] or "")),
            "signature_valid": verify(secret, d["signature"], d["body"]),
            "tampered_body_rejected": not verify(secret, d["signature"], d["body"].replace('"added":', '"added": ', 1)),
            "monitor_id_matches": data.get("monitor_id") == mid, "url": data.get("url"), "keys": sorted(data),
            "seconds_run_to_seen": secs(change["t_hook"] - change["t_run"]),
            "seconds_run_to_signed": t - int(change["t_run"] // 1000),
            "signed_within_1s_of_change": abs(t * 1000 - ms(data["at"])) < 1000,
        }
        out["change"] = {
            "after_runs": change["n"], "change_id": data["change_id"], "added": data["added"], "removed": data["removed"],
            "diff_bytes": len(data["diff"].encode()), "diff_lines": len(lines),
            "removed_lines": [l[1:] for l in lines if re.match(r"^-(?!--)", l)],
            "added_lines": [l[1:] for l in lines if re.match(r"^\+(?!\+\+)", l)],
            "diff_has_ignored_text": bool(re.search(r"Tags:|\(about\)", data["diff"])),
            "api_diff_equal": full["diff"] == data["diff"],
            "api_counts_equal": full["added"] == data["added"] and full["removed"] == data["removed"],
            "snapshot_equals_after": snapshot == after,
            "before_lines": len(before.split("\n")) if before is not None else None, "after_lines": len(after.split("\n")),
            "after_text": after,
        }

    # 4. The history: every change, newest first, with a preview of its diff.
    history = api("GET", f"/v1/monitors/{mid}/changes").json()
    m = api("GET", f"/v1/monitors/{mid}").json()
    out["history"] = {
        "changes": len(history["changes"]), "change_ids": [x["change_id"] for x in history["changes"]],
        "newest_is_webhook_change": bool(history["changes"]) and history["changes"][0]["change_id"] == out.get("change", {}).get("change_id"),
        "monitor_checks": m["checks"], "monitor_changes": m["changes"], "last_status": m["last_status"],
        "next_run_minutes_after_last_check": round((ms(m["next_run_at"]) - ms(m["last_run_at"])) / 60000),
    }

    # 5. Pause it (PATCH active: false): no more scheduled checks, history kept.
    paused = api("PATCH", f"/v1/monitors/{mid}", {"active": False})
    pj = paused.json()
    out["pause"] = {"status": paused.status_code, "active": pj.get("active"), "next_run_at": pj.get("next_run_at")}

    # 6. What the checks cost. Each check is a scrape the service runs as you, billed like any call: it has no
    # x-call-id you can see, but it is in the account's session history as engine "function:scrape".
    time.sleep(3)
    sessions = requests.get(f"{API}/v1/me/sessions", headers=HEADERS, params={"from": since, "limit": 200}, timeout=60).json()["sessions"]
    out["billing"] = []
    for ch in checks:
        inside = sorted((s for s in sessions if s["engine"] == "function:scrape"
                         and ch["t_run"] - 2000 <= ms(s["started_at"]) <= ms(ch["last_run_at"])), key=lambda s: ms(s["started_at"]))
        s = inside[0] if inside else None  # the first scrape to start after the run request
        dur = ms(s["ended_at"]) - ms(s["started_at"]) if s and s.get("ended_at") else None
        out["billing"].append({
            "check": ch["n"], "sessions_in_window": len(inside), "engine": s and s["engine"], "weight": s and s["weight"],
            "seconds": secs(dur) if dur is not None else None,
            "thread_seconds": secs(dur * s["weight"]) if dur is not None else None,
        })
    out["checks"] = [{k: v for k, v in ch.items() if k not in ("t_run", "last_run_at")} for ch in checks]
finally:
    # 7. Delete it, whatever happened above.
    deleted = api("DELETE", f"/v1/monitors/{mid}")
    gone = api("GET", f"/v1/monitors/{mid}")
    out["deleted"] = {"status": deleted.status_code, "deleted": deleted.json().get("deleted") is True, "get_after": gone.status_code}

out["measured_at"] = datetime.now(timezone.utc).strftime("%Y-%m-%d")
print(json.dumps(out, indent=2, ensure_ascii=False))
