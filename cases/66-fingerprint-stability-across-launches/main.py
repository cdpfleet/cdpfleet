# pip install playwright==1.60.0 requests
# env: CDPFLEET_API_KEY, PROXY_URL
import json
import os
import secrets
import time

import requests
from playwright.sync_api import sync_playwright

KEY = os.environ["CDPFLEET_API_KEY"]
RUN = secrets.token_hex(3)
ENGINES = [("chrome", "chromium"), ("edge", "chromium"), ("chromium", "chromium"), ("brave", "chromium"),
           ("camoufox", "firefox"), ("firefox", "firefox"), ("webkit", "webkit")]
LAUNCHES = 3
# Brave and Camoufox key their noise to the site, so the script runs on a real page.
SITE = "https://example.com/"

# What a fingerprinting script computes: canvas, WebGL (renderer + a rendered image), audio and a
# font list, each as a short hash; canvas and audio are drawn twice to see whether a session is
# consistent with itself. The same script runs in every language.
FP = """(async () => {
  const fnv = (s) => { let h = 0x811c9dc5; for (let i = 0; i < s.length; i++) { h ^= s.charCodeAt(i); h = Math.imul(h, 0x01000193) >>> 0; } return h.toString(16).padStart(8, '0'); };
  const draw = () => {
    const c = document.createElement('canvas'); c.width = 240; c.height = 60;
    const x = c.getContext('2d'); x.textBaseline = 'top'; x.font = '16px Arial';
    x.fillStyle = '#f60'; x.fillRect(100, 1, 62, 20);
    x.fillStyle = '#069'; x.fillText('cdpfleet fp 66 <@>', 2, 15);
    x.fillStyle = 'rgba(102, 204, 0, 0.7)'; x.fillText('cdpfleet fp 66 <@>', 4, 17);
    return c.toDataURL();
  };
  const webgl = () => {
    try {
      const c = document.createElement('canvas'); c.width = 64; c.height = 64;
      const gl = c.getContext('webgl', { preserveDrawingBuffer: true });
      if (!gl) return { renderer: null, image: null };
      const dbg = gl.getExtension('WEBGL_debug_renderer_info');
      const renderer = dbg ? gl.getParameter(dbg.UNMASKED_RENDERER_WEBGL) : gl.getParameter(gl.RENDERER);
      const sh = (type, src) => { const s = gl.createShader(type); gl.shaderSource(s, src); gl.compileShader(s); return s; };
      const pr = gl.createProgram();
      gl.attachShader(pr, sh(gl.VERTEX_SHADER, 'attribute vec2 p;varying vec2 v;void main(){v=p;gl_Position=vec4(p,0.0,1.0);}'));
      gl.attachShader(pr, sh(gl.FRAGMENT_SHADER, 'precision mediump float;varying vec2 v;void main(){gl_FragColor=vec4(sin(v.x*7.1)*0.5+0.5,cos(v.y*5.3)*0.5+0.5,fract(v.x*v.y*13.7),1.0);}'));
      gl.linkProgram(pr); gl.useProgram(pr);
      gl.bindBuffer(gl.ARRAY_BUFFER, gl.createBuffer());
      gl.bufferData(gl.ARRAY_BUFFER, new Float32Array([-1, -1, 1, -1, 0, 1]), gl.STATIC_DRAW);
      const loc = gl.getAttribLocation(pr, 'p'); gl.enableVertexAttribArray(loc); gl.vertexAttribPointer(loc, 2, gl.FLOAT, false, 0, 0);
      gl.drawArrays(gl.TRIANGLES, 0, 3);
      const px = new Uint8Array(64 * 64 * 4); gl.readPixels(0, 0, 64, 64, gl.RGBA, gl.UNSIGNED_BYTE, px);
      return { renderer, image: fnv(px.join(',')) };
    } catch (e) { return { renderer: null, image: null }; }
  };
  const audio = async () => {
    try {
      const ctx = new OfflineAudioContext(1, 44100, 44100);
      const o = ctx.createOscillator(); o.type = 'triangle'; o.frequency.value = 10000;
      const k = ctx.createDynamicsCompressor();
      k.threshold.value = -50; k.knee.value = 40; k.ratio.value = 12; k.attack.value = 0; k.release.value = 0.25;
      o.connect(k); k.connect(ctx.destination); o.start(0);
      // copyFromChannel into our own array: Camoufox runs this script in an isolated world, where
      // reading the page's audio buffer directly is refused.
      const d = new Float32Array(500); (await ctx.startRendering()).copyFromChannel(d, 0, 4500);
      let s = ''; for (let i = 0; i < 500; i++) s += d[i].toFixed(10) + ',';
      return fnv(s);
    } catch (e) { return 'error: ' + e.name; }
  };
  // Width probe: a family counts as installed when it measures differently from all three fallbacks.
  const FAMILIES = ['Arial', 'Arial Black', 'Calibri', 'Cambria', 'Candara', 'Comic Sans MS', 'Consolas', 'Courier New', 'DejaVu Sans', 'Georgia', 'Helvetica', 'Helvetica Neue', 'Impact', 'Liberation Sans', 'Lucida Console', 'Lucida Grande', 'Menlo', 'Monaco', 'Noto Sans', 'Palatino', 'Segoe UI', 'Tahoma', 'Times New Roman', 'Trebuchet MS', 'Ubuntu', 'Verdana', 'Roboto', 'Open Sans', 'Fira Code', 'Avenir'];
  const fonts = () => {
    const c = document.createElement('canvas').getContext('2d');
    const w = (f) => { c.font = '48px ' + f; return c.measureText('mmmmmmmmmmlli1WQ@#').width; };
    const base = ['monospace', 'serif', 'sans-serif'].map((b) => [b, w(b)]);
    return FAMILIES.filter((f) => base.some(([b, bw]) => w(JSON.stringify(f) + ', ' + b) !== bw));
  };
  const c1 = draw(), c2 = draw();
  const a1 = await audio(), a2 = await audio();
  const g = webgl();
  const found = fonts();
  return {
    canvas: fnv(c1), canvas_same_in_session: c1 === c2,
    webgl_renderer: g.renderer, webgl_image: g.image,
    audio: a1, audio_same_in_session: a1 === a2,
    fonts: fnv(found.join(',')), font_count: found.length,
    user_agent: navigator.userAgent,
  };
})()"""


def api(method, path):
    return requests.request(method, f"https://cdpfleet.com{path}", headers={"x-api-key": KEY}, timeout=30)


# The node saves a profile after the browser exits: wait for the new version before relaunching.
def wait_for_version(name, version):
    for _ in range(60):
        p = next((x for x in api("GET", "/v1/me/profiles").json().get("profiles", []) if x["name"] == name), None)
        if p and p["version"] >= version and not p["in_use"]:
            return
        time.sleep(2)
    raise RuntimeError(f"profile {name} never reached version {version}")


# 503 = the fleet is momentarily busy: wait Retry-After and try again. 409 profile_version_downgrade =
# the profile was saved by a newer build than this server has (mid-rollout): another try lands elsewhere.
def launch(engine, body):
    for attempt in range(1, 6):
        r = requests.post(f"https://starter.cdpfleet.com/{engine}/session", headers={"x-api-key": KEY}, json=body, timeout=120)
        if r.ok:
            return r.json()
        retry = r.status_code == 503 or (r.status_code == 409 and "profile_version_downgrade" in r.text)
        if not retry or attempt == 5:
            raise RuntimeError(f"{engine} launch {r.status_code} {r.text}")
        time.sleep(int(r.headers.get("retry-after") or 5))


# Proxies drop a tunnel now and then: up to two retries.
def go(page):
    for attempt in range(1, 4):
        try:
            return page.goto(SITE, timeout=60000)
        except Exception:
            if attempt == 3:
                raise
            time.sleep(2)


def measure(pw, engine, family, profile=None):
    body = {"proxy": os.environ["PROXY_URL"], "headless": True, "record": False}
    if profile:
        body["profile"] = profile
    s = launch(engine, body)
    browser = getattr(pw, family).connect(s["wsUrl"], headers={"x-api-key": KEY})
    try:
        # A profile lives in browser.contexts[0]; without one, a fresh context.
        context = browser.contexts[0] if profile else browser.new_context()
        page = context.pages[0] if context.pages else context.new_page()
        go(page)
        return page.evaluate(FP)
    finally:
        browser.close()


out = {"launches": {}, "profile": {}}
with sync_playwright() as pw:
    for engine, family in ENGINES:
        out["launches"][engine] = [measure(pw, engine, family) for _ in range(LAUNCHES)]
        # Two launches with one new profile: what does it keep?
        name = f"case66-{engine}-{RUN}"
        try:
            first = measure(pw, engine, family, name)
            wait_for_version(name, 1)
            out["profile"][engine] = [first, measure(pw, engine, family, name)]
            wait_for_version(name, 2)
        finally:
            api("DELETE", f"/v1/me/profiles/{name}")
print(json.dumps(out, indent=2, ensure_ascii=False))
