// Maven: com.microsoft.playwright:playwright:1.60.0, com.google.code.gson:gson:2.11.0
// Run with PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1. env: CDPFLEET_API_KEY, PROXY_URL
import com.google.gson.*;
import com.microsoft.playwright.*;
import java.net.URI;
import java.net.http.*;
import java.util.*;
import java.util.concurrent.*;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");
  static final HttpClient HTTP = HttpClient.newHttpClient();

  // The checks a bot-detection script typically runs, as a page script (not page.evaluate),
  // exactly as a website would run them.
  static final String CHECKS = """
      <script>
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
      </script>""";

  static JsonObject check(String name, boolean headless) throws Exception {
    String mode = headless ? "headless" : "headful";
    String body = "{\"proxy\": " + new Gson().toJson(System.getenv("PROXY_URL")) + ", \"headless\": " + headless + "}";
    HttpResponse<String> res = HTTP.send(HttpRequest.newBuilder(URI.create("https://starter.cdpfleet.com/" + name + "/session"))
        .header("x-api-key", KEY).header("content-type", "application/json")
        .POST(HttpRequest.BodyPublishers.ofString(body)).build(), HttpResponse.BodyHandlers.ofString());
    JsonObject out = new JsonObject();
    out.addProperty("build", name);
    out.addProperty("mode", mode);
    if (res.statusCode() != 200) { out.addProperty("error", res.statusCode() + " " + res.body()); return out; }
    String wsUrl = JsonParser.parseString(res.body()).getAsJsonObject().get("wsUrl").getAsString();
    try (Playwright playwright = Playwright.create()) { // Playwright objects are per thread
      Browser browser = playwright.chromium().connect(wsUrl, new BrowserType.ConnectOptions().setHeaders(Map.of("x-api-key", KEY)));
      try {
        Page page = browser.newPage();
        // A real https origin: some APIs (permissions, WebGL info) behave differently on about:blank.
        page.route("https://detect.example/", route -> route.fulfill(new Route.FulfillOptions().setContentType("text/html").setBody(CHECKS)));
        page.navigate("https://detect.example/");
        out.addProperty("version", browser.version());
        JsonObject checks = new Gson().toJsonTree(page.evaluate("window.__checks")).getAsJsonObject();
        for (String k : checks.keySet()) out.add(k, checks.get(k));
        return out;
      } finally {
        browser.close();
      }
    }
  }

  public static void main(String[] args) throws Exception {
    // Every build twice: headless (1 thread) and headful on a virtual display (2 threads).
    List<Callable<JsonObject>> jobs = new ArrayList<>();
    for (String b : List.of("chromium", "chrome", "patchright", "cloakbrowser")) {
      jobs.add(() -> check(b, true));
      jobs.add(() -> check(b, false));
    }
    ExecutorService pool = Executors.newFixedThreadPool(jobs.size());
    JsonArray rows = new JsonArray();
    for (Future<JsonObject> f : pool.invokeAll(jobs)) rows.add(f.get());
    pool.shutdown();
    System.out.println(new GsonBuilder().setPrettyPrinting().disableHtmlEscaping().create().toJson(rows));
  }
}
