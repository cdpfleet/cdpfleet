# pip install playwright==1.60.0 requests
# env: CDPFLEET_API_KEY, PROXY_URL
import json
import os
from concurrent.futures import ThreadPoolExecutor

import requests
from playwright.sync_api import sync_playwright

KEY = os.environ["CDPFLEET_API_KEY"]
BUILDS = ["chromium", "chrome", "patchright", "cloakbrowser"]

# The checks a bot-detection script typically runs, as a page script (not page.evaluate),
# exactly as a website would run them.
CHECKS = """<script>
window.__checks = (async () => {
  const gl = document.createElement('canvas').getContext('webgl');
  const dbg = gl && gl.getExtension('WEBGL_debug_renderer_info');
  const perm = await navigator.permissions.query({ name: 'notifications' });
  return {
    webdriver: navigator.webdriver,
    headless_in_ua: /Headless/.test(navigator.userAgent),
    window_chrome: typeof window.chrome === 'object',
    plugins: navigator.plugins.length,
    languages: navigator.languages.join(','),
    webgl_renderer: dbg ? gl.getParameter(dbg.UNMASKED_RENDERER_WEBGL) : null,
    notification_permission_mismatch: Notification.permission === 'denied' && perm.state === 'prompt',
    outer_minus_inner_height: outerHeight - innerHeight,
  };
})();
</script>"""


def check(job):
    name, headless = job
    mode = "headless" if headless else "headful"
    res = requests.post(f"https://starter.cdpfleet.com/{name}/session", headers={"x-api-key": KEY},
                        json={"proxy": os.environ["PROXY_URL"], "headless": headless}, timeout=60)
    if not res.ok:
        return {"build": name, "mode": mode, "error": f"{res.status_code} {res.text}"}
    with sync_playwright() as p:  # the sync API is per thread
        browser = p.chromium.connect(res.json()["wsUrl"], headers={"x-api-key": KEY})
        try:
            page = browser.new_page()
            # A real https origin: some APIs (permissions, WebGL info) behave differently on about:blank.
            page.route("https://detect.example/", lambda route: route.fulfill(content_type="text/html", body=CHECKS))
            page.goto("https://detect.example/")
            return {"build": name, "mode": mode, "version": browser.version, **page.evaluate("window.__checks")}
        finally:
            browser.close()


# Every build twice: headless (1 thread) and headful on a virtual display (2 threads).
jobs = [(b, h) for b in BUILDS for h in (True, False)]
with ThreadPoolExecutor(len(jobs)) as pool:
    print(json.dumps(list(pool.map(check, jobs)), indent=2))
