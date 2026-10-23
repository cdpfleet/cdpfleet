// Maven: com.microsoft.playwright:playwright:1.60.0, com.google.code.gson:gson:2.11.0
// Run with PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1.
// env: CDPFLEET_API_KEY, PROXY_URL
import com.google.gson.*;
import com.microsoft.playwright.*;
import java.net.URI;
import java.net.http.*;
import java.security.SecureRandom;
import java.util.*;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");
  static final Gson GSON = new GsonBuilder().setPrettyPrinting().disableHtmlEscaping().serializeNulls().create();
  static final HttpClient HTTP = HttpClient.newHttpClient();
  static final String RUN = HexFormat.of().formatHex(new SecureRandom().generateSeed(3));
  static final String[][] ENGINES = {{"chrome", "chromium"}, {"edge", "chromium"}, {"chromium", "chromium"}, {"brave", "chromium"},
      {"camoufox", "firefox"}, {"firefox", "firefox"}, {"webkit", "webkit"}};
  static final int LAUNCHES = 3;
  // Brave and Camoufox key their noise to the site, so the script runs on a real page.
  static final String SITE = "https://example.com/";

  // What a fingerprinting script computes: canvas, WebGL (renderer + a rendered image), audio and a
  // font list, each as a short hash; canvas and audio are drawn twice to see whether a session is
  // consistent with itself. The same script runs in every language.
  static final String FP = """
(async () => {
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
})()
""";

  static HttpResponse<String> api(String method, String path) throws Exception {
    return HTTP.send(HttpRequest.newBuilder(URI.create("https://cdpfleet.com" + path)).header("x-api-key", KEY).method(method, HttpRequest.BodyPublishers.noBody()).build(), HttpResponse.BodyHandlers.ofString());
  }

  // The node saves a profile after the browser exits: wait for the new version before relaunching.
  static void waitForVersion(String name, int version) throws Exception {
    for (int i = 0; i < 60; i++) {
      for (JsonElement e : JsonParser.parseString(api("GET", "/v1/me/profiles").body()).getAsJsonObject().getAsJsonArray("profiles")) {
        JsonObject p = e.getAsJsonObject();
        if (p.get("name").getAsString().equals(name) && p.get("version").getAsInt() >= version && !p.get("in_use").getAsBoolean()) return;
      }
      Thread.sleep(2000);
    }
    throw new RuntimeException("profile " + name + " never reached version " + version);
  }

  // 503 = the fleet is momentarily busy: wait Retry-After and try again. 409 profile_version_downgrade =
  // the profile was saved by a newer build than this server has (mid-rollout): another try lands elsewhere.
  static JsonObject launch(String engine, JsonObject body) throws Exception {
    for (int attempt = 1; ; attempt++) {
      HttpResponse<String> res = HTTP.send(HttpRequest.newBuilder(URI.create("https://starter.cdpfleet.com/" + engine + "/session"))
          .header("x-api-key", KEY).header("content-type", "application/json")
          .POST(HttpRequest.BodyPublishers.ofString(body.toString())).build(), HttpResponse.BodyHandlers.ofString());
      if (res.statusCode() == 200) return JsonParser.parseString(res.body()).getAsJsonObject();
      boolean retry = res.statusCode() == 503 || (res.statusCode() == 409 && res.body().contains("profile_version_downgrade"));
      if (!retry || attempt == 5) throw new RuntimeException(engine + " launch " + res.statusCode() + " " + res.body());
      Thread.sleep(Long.parseLong(res.headers().firstValue("retry-after").orElse("5")) * 1000);
    }
  }

  // Proxies drop a tunnel now and then: up to two retries.
  static void go(Page page) throws Exception {
    for (int attempt = 1; ; attempt++) {
      try {
        page.navigate(SITE, new Page.NavigateOptions().setTimeout(60000));
        return;
      } catch (PlaywrightException err) {
        if (attempt == 3) throw err;
        Thread.sleep(2000);
      }
    }
  }

  static BrowserType type(Playwright pw, String family) {
    return family.equals("firefox") ? pw.firefox() : family.equals("webkit") ? pw.webkit() : pw.chromium();
  }

  static JsonElement measure(Playwright pw, String engine, String family, String profile) throws Exception {
    JsonObject body = new JsonObject();
    body.addProperty("proxy", System.getenv("PROXY_URL"));
    body.addProperty("headless", true);
    body.addProperty("record", false);
    if (profile != null) body.addProperty("profile", profile);
    JsonObject s = launch(engine, body);
    Browser browser = type(pw, family).connect(s.get("wsUrl").getAsString(), new BrowserType.ConnectOptions().setHeaders(Map.of("x-api-key", KEY)));
    try {
      // A profile lives in browser.contexts().get(0); without one, a fresh context.
      BrowserContext context = profile != null ? browser.contexts().get(0) : browser.newContext();
      Page page = context.pages().isEmpty() ? context.newPage() : context.pages().get(0);
      go(page);
      return GSON.toJsonTree(page.evaluate(FP));
    } finally {
      browser.close();
    }
  }

  public static void main(String[] args) throws Exception {
    JsonObject launches = new JsonObject(), profiles = new JsonObject();
    try (Playwright pw = Playwright.create()) {
      for (String[] e : ENGINES) {
        JsonArray runs = new JsonArray();
        for (int i = 0; i < LAUNCHES; i++) runs.add(measure(pw, e[0], e[1], null));
        launches.add(e[0], runs);
        // Two launches with one new profile: what does it keep?
        String name = "case66-" + e[0] + "-" + RUN;
        try {
          JsonArray pair = new JsonArray();
          pair.add(measure(pw, e[0], e[1], name));
          waitForVersion(name, 1);
          pair.add(measure(pw, e[0], e[1], name));
          waitForVersion(name, 2);
          profiles.add(e[0], pair);
        } finally {
          api("DELETE", "/v1/me/profiles/" + name);
        }
      }
    }
    JsonObject out = new JsonObject();
    out.add("launches", launches);
    out.add("profile", profiles);
    System.out.println(GSON.toJson(out));
  }
}
