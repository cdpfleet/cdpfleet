// Maven: com.microsoft.playwright:playwright:1.60.0, com.google.code.gson:gson:2.11.0
// Run with PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1.
// env: CDPFLEET_API_KEY, PROXY_URL
import com.google.gson.*;
import com.microsoft.playwright.*;
import java.net.URI;
import java.net.http.*;
import java.util.*;
import java.util.concurrent.*;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");
  static final String PROXY = System.getenv("PROXY_URL");
  // Nulls matter here (no deviceMemory in Firefox, no WebGL renderer): keep them in the JSON.
  static final Gson GSON = new GsonBuilder().serializeNulls().disableHtmlEscaping().create();

  // Four ways to run a browser; the same twelve signals read from inside the page.
  record Target(String label, String engine, String family, String body, String prefix) {}
  static final Target[] TARGETS = {
    new Target("Chromium, headless", "chromium", "chromium", "{\"headless\": \"new\"}", ""),
    new Target("Chromium, headful", "chromium", "chromium", "{\"headless\": false}", ""),
    new Target("Patchright, headful", "patchright", "chromium", "{\"headless\": false}", ""),
    // Camoufox: read the page's own world ("mw:" needs main_world_eval), like a site would.
    new Target("Camoufox, headful", "camoufox", "firefox", "{\"headless\": false, \"os\": \"windows\", \"main_world_eval\": true}", "mw:"),
  };

  // The classic headless and automation tells, read the way a detection script reads them.
  static final String SIGNALS = """
      (async () => {
        let query = null;
        try { query = (await navigator.permissions.query({ name: 'notifications' })).state; } catch { query = 'error'; }
        const gl = (() => {
          try { const g = document.createElement('canvas').getContext('webgl'); const d = g.getExtension('WEBGL_debug_renderer_info'); return g.getParameter(d ? d.UNMASKED_RENDERER_WEBGL : g.RENDERER); } catch { return null; }
        })();
        return {
          ua_says_headless: /Headless/.test(navigator.userAgent),
          webdriver: navigator.webdriver,
          plugins: navigator.plugins.length,
          notification_mismatch: typeof Notification !== 'undefined' && Notification.permission === 'denied' && query === 'prompt',
          outer_window_zero: outerWidth === 0 || outerHeight === 0,
          screen: screen.width + 'x' + screen.height,
          webgl_renderer: gl,
          cores: navigator.hardwareConcurrency,
          memory_gb: navigator.deviceMemory ?? null,
          languages: navigator.languages.join(','),
          chrome_object: typeof window.chrome === 'object' && window.chrome !== null,
        };
      })()""";
  static final String[] TELLS = {"ua_says_headless", "webdriver", "notification_mismatch", "outer_window_zero"};

  static JsonObject inspect(Target t) throws Exception {
    JsonObject options = JsonParser.parseString(t.body()).getAsJsonObject();
    options.addProperty("proxy", PROXY);
    HttpResponse<String> res = HttpClient.newHttpClient().send(HttpRequest.newBuilder(URI.create("https://starter.cdpfleet.com/" + t.engine() + "/session"))
        .header("x-api-key", KEY).header("content-type", "application/json")
        .POST(HttpRequest.BodyPublishers.ofString(GSON.toJson(options))).build(), HttpResponse.BodyHandlers.ofString());
    JsonObject out = new JsonObject();
    out.addProperty("label", t.label());
    if (res.statusCode() != 200) { out.addProperty("error", "launch " + res.statusCode() + " " + res.body()); return out; }
    JsonObject session = JsonParser.parseString(res.body()).getAsJsonObject();
    try (Playwright playwright = Playwright.create()) { // Playwright objects are per thread
      BrowserType type = t.family().equals("firefox") ? playwright.firefox() : playwright.chromium();
      Browser browser = type.connect(session.get("wsUrl").getAsString(), new BrowserType.ConnectOptions().setHeaders(Map.of("x-api-key", KEY)));
      try {
        Page page = browser.newPage();
        page.navigate("https://example.com/", new Page.NavigateOptions().setTimeout(60000));
        JsonObject signals = GSON.toJsonTree(page.evaluate(t.prefix() + SIGNALS)).getAsJsonObject();
        out.add("threads", session.get("weight"));
        for (String k : signals.keySet()) out.add(k, signals.get(k));
        List<String> tells = new ArrayList<>();
        for (String k : TELLS) if (signals.has(k) && signals.get(k).isJsonPrimitive() && signals.get(k).getAsJsonPrimitive().isBoolean() && signals.get(k).getAsBoolean()) tells.add(k);
        out.addProperty("tells", tells.isEmpty() ? "none" : String.join(", ", tells));
        return out;
      } finally {
        browser.close();
      }
    }
  }

  public static void main(String[] args) throws Exception {
    ExecutorService pool = Executors.newFixedThreadPool(TARGETS.length);
    List<Future<JsonObject>> jobs = new ArrayList<>();
    for (Target t : TARGETS) jobs.add(pool.submit(() -> inspect(t)));
    JsonArray rows = new JsonArray();
    for (Future<JsonObject> f : jobs) rows.add(f.get());
    pool.shutdown();
    System.out.println(new GsonBuilder().serializeNulls().setPrettyPrinting().disableHtmlEscaping().create().toJson(rows));
  }
}
