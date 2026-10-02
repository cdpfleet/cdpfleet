# pip install playwright==1.60.0 requests
# env: CDPFLEET_API_KEY, PROXY_URL
import hashlib
import json
import os
import secrets
import tempfile
import time

import requests
from playwright.sync_api import sync_playwright

KEY = os.environ["CDPFLEET_API_KEY"]
sha256 = lambda b: hashlib.sha256(b).hexdigest()

# A local file to upload: 2,000 CSV rows (~60 KB).
upload_path = os.path.join(tempfile.gettempdir(), "cdpfleet-upload.csv")
with open(upload_path, "w") as f:
    f.write("id,token\n" + "".join(f"{i},{secrets.token_hex(12)}\n" for i in range(1, 2001)))
local = open(upload_path, "rb").read()

res = requests.post("https://starter.cdpfleet.com/chromium/session", headers={"x-api-key": KEY},
                    json={"proxy": os.environ["PROXY_URL"], "headless": True}, timeout=60)
res.raise_for_status()

# A small page on httpbin.org's origin with an upload form and a download link.
PAGE = """<form method="post" action="/anything" enctype="multipart/form-data">
  <input type="file" name="upload" id="file"><button id="send">Send</button></form>
<a id="data" href="/bytes/102400?seed=42" download="data.bin">data</a>"""

with sync_playwright() as p:
    browser = p.chromium.connect(res.json()["wsUrl"], headers={"x-api-key": KEY})
    try:
        page = browser.new_page()
        page.route("https://httpbin.org/files-demo", lambda route: route.fulfill(content_type="text/html", body=PAGE))
        page.goto("https://httpbin.org/files-demo", timeout=60000)

        # Upload: set_input_files reads the file HERE and streams it to the remote browser.
        t = time.time()
        page.set_input_files("#file", upload_path)
        with page.expect_navigation(timeout=60000) as nav:
            page.click("#send")
        echoed = nav.value.json()["files"]["upload"].encode()
        upload_ms = round((time.time() - t) * 1000)

        # Download: the file lands on the remote server; save_as streams it back here.
        page.goto("https://httpbin.org/files-demo", timeout=60000)
        t = time.time()
        with page.expect_download(timeout=60000) as dl:
            page.click("#data")
        download_path = os.path.join(tempfile.gettempdir(), dl.value.suggested_filename)
        dl.value.save_as(download_path)
        download_ms = round((time.time() - t) * 1000)
        got = open(download_path, "rb").read()
        # The same seeded bytes fetched directly from here, to prove the copy is exact.
        direct = requests.get("https://httpbin.org/bytes/102400?seed=42", timeout=60).content

        print(json.dumps({
            "upload": {"local_file": os.path.basename(upload_path), "bytes": len(local), "sha256": sha256(local),
                       "server_received_bytes": len(echoed), "server_sha256": sha256(echoed),
                       "identical": sha256(local) == sha256(echoed), "ms": upload_ms},
            "download": {"suggested_filename": dl.value.suggested_filename, "bytes": len(got), "sha256": sha256(got),
                         "direct_sha256": sha256(direct), "identical": sha256(got) == sha256(direct), "ms": download_ms},
        }, indent=2))
    finally:
        browser.close()
