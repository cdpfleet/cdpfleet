# pip install playwright==1.60.0 requests
# env: CDPFLEET_API_KEY, PROXY_URL
import base64
import json
import os
import time

import requests
from playwright.sync_api import sync_playwright

KEY = os.environ["CDPFLEET_API_KEY"]
SITE = "https://practicesoftwaretesting.com"  # Toolshop: a public demo shop built for test automation
API = "https://api.practicesoftwaretesting.com"
LOGIN = {"email": "customer@practicesoftwaretesting.com", "password": "welcome01"}  # the site's published demo account

# Fetches every page of the API from inside the page with the token: one by one, then all at once.
FETCH_ALL = """async ({ api, token }) => {
  const get = (n) => fetch(`${api}/invoices?page=${n}`, { headers: { authorization: `Bearer ${token}` } }).then(async (r) => ({ status: r.status, text: await r.text() }));
  const meta = JSON.parse((await get(1)).text);
  const numbers = Array.from({ length: meta.last_page }, (_, i) => i + 1);
  let t = performance.now();
  const pages = [];
  for (const n of numbers) pages.push(await get(n));
  const sequential = (performance.now() - t) / 1000;
  t = performance.now();
  await Promise.all(numbers.map(get));
  const parallel = (performance.now() - t) / 1000;
  const noToken = await fetch(`${api}/invoices?page=1`).then((r) => r.status);
  const items = pages.flatMap((p) => JSON.parse(p.text).data);
  return {
    sequential, parallel, total: meta.total, perPage: meta.per_page, lastPage: meta.last_page,
    requests: pages.length, bytes: pages.reduce((s, p) => s + p.text.length, 0), statuses: [...new Set(pages.map((p) => p.status))],
    invoices: items.map((i) => i.invoice_number), fields: Object.keys(items[0]).length, noToken,
  };
}"""


def launch(body):
    # 503 means the fleet is momentarily busy: wait Retry-After and try again.
    for attempt in range(1, 6):
        res = requests.post("https://starter.cdpfleet.com/chromium/session", headers={"x-api-key": KEY}, json=body)
        if res.ok:
            return res.json()
        if res.status_code != 503 or attempt == 5:
            raise RuntimeError(f"launch {res.status_code} {res.text}")
        time.sleep(int(res.headers.get("retry-after") or 5))


session = launch({"proxy": os.environ["PROXY_URL"], "headless": "new", "record": False})
with sync_playwright() as p:
    browser = p.chromium.connect(session["wsUrl"], headers={"x-api-key": KEY})
    try:
        page = browser.new_page()
        # The app can take 30 s to render through a rotating residential exit: wait up to 90 s.
        page.set_default_timeout(90000)

        # 1. Log in like a person; the site stores its token and sends it with every API call.
        page.goto(f"{SITE}/auth/login")
        page.fill("#email", LOGIN["email"])
        page.fill("#password", LOGIN["password"])
        page.click("[type=submit]")
        page.wait_for_url("**/account")

        # 2. Find the token twice: in localStorage, and in the header of the page's own API request.
        stored = page.evaluate("() => localStorage.getItem('auth-token')")
        ui_calls = 0

        def count_call(r):
            global ui_calls
            if r.url.startswith(f"{API}/invoices"):
                ui_calls += 1

        page.on("request", count_call)
        with page.expect_request(lambda r: r.url.startswith(f"{API}/invoices")) as first_call:
            page.goto(f"{SITE}/account/invoices")
        header = first_call.value.all_headers().get("authorization", "")
        payload = stored.split(".")[1]
        claims = json.loads(base64.urlsafe_b64decode(payload + "=" * (-len(payload) % 4)))

        # 3. The page's way: click through the pager, reading the table each time.
        page.wait_for_selector("table tbody tr")
        t0 = time.time()
        read_table = lambda: page.eval_on_selector_all("table tbody tr td:first-child", "tds => tds.map((td) => td.textContent.trim())")
        via_ui = read_table()
        last_page = int(page.locator('button[aria-label^="Page-"]').last.inner_text())
        for n in range(2, last_page + 1):
            with page.expect_response(lambda r, n=n: r.url == f"{API}/invoices?page={n}"):
                page.click('[data-test="pagination-next"]')
            page.wait_for_function("first => document.querySelector('table tbody tr td')?.textContent.trim() !== first", arg=via_ui[-15])
            via_ui += read_table()
        ui_seconds = time.time() - t0
        page.remove_listener("request", count_call)
        columns = page.locator("table thead th").count()

        # 4. The API's way: the same token on fetch() from inside the page — the browser makes the
        #    requests, through the session's proxy.
        via_api = page.evaluate(FETCH_ALL, {"api": API, "token": stored})

        print(json.dumps({
            "token": {
                "in_local_storage": "auth-token" if stored else None,
                "header_matches": header == f"Bearer {stored}",
                "lifetime_seconds": claims["exp"] - claims["iat"],
                "seconds_left_at_end": claims["exp"] - int(time.time()),
            },
            "api": {"total": via_api["total"], "per_page": via_api["perPage"], "last_page": via_api["lastPage"], "without_token": via_api["noToken"]},
            "ui": {"pages": last_page, "invoices": len(via_ui), "invoice_calls": ui_calls, "columns": columns, "seconds": ui_seconds},
            "direct": {"requests": via_api["requests"], "statuses": via_api["statuses"], "invoices": len(via_api["invoices"]), "fields_per_invoice": via_api["fields"],
                       "json_bytes": via_api["bytes"], "seconds_one_by_one": via_api["sequential"], "seconds_all_at_once": via_api["parallel"]},
            "same_invoices": len(via_ui) == len(via_api["invoices"]) and sorted(via_ui) == sorted(via_api["invoices"]),
            "ui_missing_from_api": len(set(via_ui) - set(via_api["invoices"])),
        }))
    finally:
        browser.close()
