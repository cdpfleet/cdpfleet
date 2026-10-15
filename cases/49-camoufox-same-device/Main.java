// Maven: com.microsoft.playwright:playwright:1.60.0, com.google.code.gson:gson:2.11.0
// Run with PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1.
// env: CDPFLEET_API_KEY, PROXY_URL_US
import com.google.gson.*;
import com.microsoft.playwright.*;
import java.net.URI;
import java.net.http.*;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.security.SecureRandom;
import java.util.*;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");
  static final Gson GSON = new GsonBuilder().setPrettyPrinting().disableHtmlEscaping().serializeNulls().create();
  static final HttpClient HTTP = HttpClient.newHttpClient();
  static final String NAME = "case49-java-" + HexFormat.of().formatHex(new SecureRandom().generateSeed(4)); // one profile per run
  static final String SITE = "https://httpbin.org/html";

  static final String FINGERPRINT = "() => {"
      + "const gl = document.createElement('canvas').getContext('webgl');"
      + "const dbg = gl && gl.getExtension('WEBGL_debug_renderer_info');"
      + "const draw = () => { const c = document.createElement('canvas'); c.width = 240; c.height = 60; const ctx = c.getContext('2d');"
      + " ctx.font = '18px Arial'; ctx.fillStyle = '#f60'; ctx.fillRect(10, 10, 100, 30); ctx.fillStyle = '#069'; ctx.fillText('cdpfleet device 49', 4, 40); return c.toDataURL(); };"
      + "return { user_agent: navigator.userAgent, platform: navigator.platform, cores: navigator.hardwareConcurrency, screen: `${screen.width}x${screen.height}`,"
      + " webgl_renderer: dbg ? gl.getParameter(dbg.UNMASKED_RENDERER_WEBGL) : null, canvas: draw(), canvas_again: draw(),"
      + " timezone: Intl.DateTimeFormat().resolvedOptions().timeZone, languages: navigator.languages,"
      + " login_cookie: document.cookie.includes('session=alice-7f3'), login_storage: localStorage.getItem('user') === 'alice' }; }";

  static String shortHash(String text, int n) throws Exception {
    return HexFormat.of().formatHex(MessageDigest.getInstance("SHA-256").digest(text.getBytes(StandardCharsets.UTF_8))).substring(0, n);
  }

  static HttpResponse<String> api(String method, String path) throws Exception {
    return HTTP.send(HttpRequest.newBuilder(URI.create("https://cdpfleet.com" + path)).header("x-api-key", KEY).method(method, HttpRequest.BodyPublishers.noBody()).build(), HttpResponse.BodyHandlers.ofString());
  }

  // The node saves the profile after the session's browser has exited: wait for the new version.
  static void waitForVersion(int version) throws Exception {
    for (int i = 0; i < 60; i++) {
      for (JsonElement e : JsonParser.parseString(api("GET", "/v1/me/profiles").body()).getAsJsonObject().getAsJsonArray("profiles")) {
        JsonObject p = e.getAsJsonObject();
        if (p.get("name").getAsString().equals(NAME) && p.get("version").getAsInt() >= version && !p.get("in_use").getAsBoolean()) return;
      }
      Thread.sleep(2000);
    }
    throw new RuntimeException("profile " + NAME + " never reached version " + version);
  }

  // 503 means the fleet is momentarily busy: wait Retry-After and try again.
  static JsonObject launch(JsonObject body) throws Exception {
    for (int attempt = 1; ; attempt++) {
      HttpResponse<String> res = HTTP.send(HttpRequest.newBuilder(URI.create("https://starter.cdpfleet.com/camoufox/session"))
          .header("x-api-key", KEY).header("content-type", "application/json")
          .POST(HttpRequest.BodyPublishers.ofString(body.toString())).build(), HttpResponse.BodyHandlers.ofString());
      if (res.statusCode() == 200) return JsonParser.parseString(res.body()).getAsJsonObject();
      if (res.statusCode() != 503 || attempt == 5) throw new RuntimeException("launch " + res.statusCode() + " " + res.body());
      Thread.sleep(Long.parseLong(res.headers().firstValue("retry-after").orElse("5")) * 1000);
    }
  }

  // Proxies drop a tunnel now and then: up to two retries.
  static void go(Page page, String url) throws Exception {
    for (int attempt = 1; ; attempt++) {
      try {
        page.navigate(url, new Page.NavigateOptions().setTimeout(60000));
        return;
      } catch (PlaywrightException err) {
        if (attempt == 3) throw err;
        Thread.sleep(2000);
      }
    }
  }

  // What a fingerprinting script reads, plus the "login" kept in a cookie and localStorage.
  static JsonObject visit(Page page, boolean logIn) throws Exception {
    go(page, SITE);
    if (logIn) page.evaluate("() => { document.cookie = 'session=alice-7f3; max-age=604800; path=/'; localStorage.setItem('user', 'alice'); }");
    JsonObject fp = GSON.toJsonTree(page.evaluate(FINGERPRINT)).getAsJsonObject();
    String canvas = fp.get("canvas").getAsString();
    fp.addProperty("canvas_same_in_session", canvas.equals(fp.get("canvas_again").getAsString()));
    fp.addProperty("canvas", shortHash(canvas, 12));
    fp.remove("canvas_again");
    return fp;
  }

  static JsonObject session(Playwright pw, JsonObject body, boolean logIn) throws Exception {
    JsonObject s = launch(body);
    Browser browser = pw.firefox().connect(s.get("wsUrl").getAsString(), new BrowserType.ConnectOptions().setHeaders(Map.of("x-api-key", KEY)));
    try {
      // With a profile, its context is browser.contexts().get(0); without one, a fresh context.
      BrowserContext context = body.has("profile") ? browser.contexts().get(0) : browser.newContext();
      Page page = context.pages().isEmpty() ? context.newPage() : context.pages().get(0);
      return visit(page, logIn);
    } finally {
      browser.close();
    }
  }

  // The device's hardware-like values (user agent, platform, cores, screen, GPU); the canvas is
  // reported on its own. Compact JSON like JavaScript's JSON.stringify, so hashes match.
  static String device(JsonObject r) throws Exception {
    JsonArray a = new JsonArray();
    a.add(r.get("user_agent")); a.add(r.get("platform")); a.add(r.get("cores").getAsInt()); a.add(r.get("screen")); a.add(r.get("webgl_renderer"));
    return shortHash(new GsonBuilder().disableHtmlEscaping().serializeNulls().create().toJson(a), 10);
  }

  static JsonObject pick(int run, JsonObject r) throws Exception {
    JsonObject o = new JsonObject();
    o.addProperty("run", run);
    o.addProperty("device", device(r));
    for (String k : new String[] {"user_agent", "cores", "screen", "webgl_renderer", "canvas", "canvas_same_in_session", "timezone", "login_cookie", "login_storage"}) o.add(k, r.get(k));
    o.addProperty("cores", r.get("cores").getAsInt());
    return o;
  }

  public static void main(String[] args) throws Exception {
    JsonObject base = new JsonObject();
    base.addProperty("proxy", System.getenv("PROXY_URL_US")); // one country: Camoufox matches timezone and locale to the exit IP
    base.addProperty("headless", true);
    base.addProperty("os", "windows");
    base.addProperty("record", false);
    try (Playwright pw = Playwright.create()) {
      // Four launches with one profile: the first creates it (device + login), the rest restore it.
      JsonArray withProfile = new JsonArray();
      for (int run = 1; run <= 4; run++) {
        JsonObject body = base.deepCopy();
        body.addProperty("profile", NAME);
        withProfile.add(pick(run, session(pw, body, run == 1)));
        waitForVersion(run);
      }
      // Two launches without a profile, for contrast.
      JsonArray without = new JsonArray();
      for (int run = 1; run <= 2; run++) without.add(pick(run, session(pw, base.deepCopy(), false)));
      JsonObject out = new JsonObject();
      out.add("with_profile", withProfile);
      out.add("without_profile", without);
      System.out.println(GSON.toJson(out));
    } finally {
      api("DELETE", "/v1/me/profiles/" + NAME);
    }
  }
}
