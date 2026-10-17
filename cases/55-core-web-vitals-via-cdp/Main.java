// Maven: com.microsoft.playwright:playwright:1.60.0, com.google.code.gson:gson:2.11.0
// Run with PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1. env: CDPFLEET_API_KEY, PROXY_URL
import com.google.gson.*;
import com.microsoft.playwright.*;
import com.microsoft.playwright.options.WaitUntilState;
import java.net.URI;
import java.net.http.*;
import java.util.*;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");
  static final String[][] PAGES = {
    {"example.com", "https://example.com/"},
    {"books.toscrape.com", "https://books.toscrape.com/"},
    {"Wikipedia article", "https://en.wikipedia.org/wiki/Web_browser"},
    {"shifting demo", "https://shift.test/"}, // served by page.route below
  };
  // A made-up page whose banner arrives late and pushes the text down: a layout shift on purpose.
  static final String SHIFTING = "<!doctype html><meta charset=\"utf-8\"><title>shift</title>\n"
      + "<body style=\"margin:0;font:18px sans-serif\"><div id=\"slot\"></div>\n<h1>Article</h1>"
      + "<p>Lorem ipsum dolor sit amet, consectetur adipiscing elit.</p>".repeat(30)
      + "\n<script>setTimeout(() => { document.getElementById('slot').innerHTML = '<div style=\"height:300px;background:#fc0\">Late banner</div>'; }, 600);</script>";

  // Reads the vitals from the page's own performance timeline. `buffered: true` returns the
  // entries recorded before the observer existed, so this runs after the load.
  static final String VITALS = """
      new Promise((resolve) => {
        const nav = performance.getEntriesByType('navigation')[0];
        const fcp = performance.getEntriesByName('first-contentful-paint')[0];
        let lcp = null, cls = 0;
        new PerformanceObserver((l) => { for (const e of l.getEntries()) lcp = e; }).observe({ type: 'largest-contentful-paint', buffered: true });
        new PerformanceObserver((l) => { for (const e of l.getEntries()) if (!e.hadRecentInput) cls += e.value; }).observe({ type: 'layout-shift', buffered: true });
        setTimeout(() => resolve(JSON.stringify({
          ttfb_ms: Math.round(nav.responseStart),
          connect_ms: Math.round(nav.connectEnd - nav.domainLookupStart),
          wait_ms: Math.round(nav.responseStart - nav.requestStart),
          fcp_ms: fcp ? Math.round(fcp.startTime) : null,
          lcp_ms: lcp ? Math.round(lcp.startTime) : null,
          lcp_element: lcp && lcp.element ? lcp.element.tagName.toLowerCase() : null,
          cls: Math.round(cls * 1000) / 1000,
          load_ms: Math.round(nav.loadEventEnd),
        })), 200);
      })""";

  public static void main(String[] args) throws Exception {
    Gson gson = new GsonBuilder().serializeNulls().create();
    String body = "{\"proxy\": " + gson.toJson(System.getenv("PROXY_URL")) + ", \"headless\": \"new\"}";
    HttpResponse<String> res = HttpClient.newHttpClient().send(HttpRequest.newBuilder(URI.create("https://starter.cdpfleet.com/chromium/session"))
        .header("x-api-key", KEY).header("content-type", "application/json")
        .POST(HttpRequest.BodyPublishers.ofString(body)).build(), HttpResponse.BodyHandlers.ofString());
    if (res.statusCode() != 200) throw new RuntimeException("launch: " + res.statusCode() + " " + res.body());
    String wsUrl = JsonParser.parseString(res.body()).getAsJsonObject().get("wsUrl").getAsString();

    try (Playwright playwright = Playwright.create()) {
      Browser browser = playwright.chromium().connect(wsUrl, new BrowserType.ConnectOptions().setHeaders(Map.of("x-api-key", KEY)));
      try {
        JsonArray rows = new JsonArray();
        for (String[] p : PAGES) {
          BrowserContext context = browser.newContext(new Browser.NewContextOptions().setViewportSize(1280, 800)); // cold cache
          context.route("https://shift.test/**", route -> route.fulfill(new Route.FulfillOptions().setContentType("text/html; charset=utf-8").setBody(SHIFTING)));
          Page page = context.newPage();
          // A CDP session over the normal Playwright connection: the browser's own counters.
          CDPSession cdp = context.newCDPSession(page);
          cdp.send("Performance.enable");
          page.navigate(p[1], new Page.NavigateOptions().setWaitUntil(WaitUntilState.LOAD).setTimeout(60000));
          page.waitForTimeout(1500); // let late content (and late shifts) land
          JsonObject row = new JsonObject();
          row.addProperty("page", p[0]);
          // The vitals come back as a JSON string, which Gson turns into the row directly.
          JsonObject vitals = JsonParser.parseString((String) page.evaluate(VITALS)).getAsJsonObject();
          for (Map.Entry<String, JsonElement> e : vitals.entrySet()) row.add(e.getKey(), e.getValue());
          Map<String, Double> metrics = new HashMap<>();
          for (JsonElement m : cdp.send("Performance.getMetrics").getAsJsonArray("metrics"))
            metrics.put(m.getAsJsonObject().get("name").getAsString(), m.getAsJsonObject().get("value").getAsDouble());
          row.addProperty("script_ms", Math.round(metrics.get("ScriptDuration") * 1000));
          row.addProperty("layouts", metrics.get("LayoutCount").longValue());
          row.addProperty("dom_nodes", metrics.get("Nodes").longValue());
          row.addProperty("js_heap_mb", Math.round(metrics.get("JSHeapUsedSize") / 1048576 * 10) / 10.0);
          rows.add(row);
          context.close();
        }
        JsonObject out = new JsonObject();
        out.add("rows", rows);
        System.out.println(new GsonBuilder().serializeNulls().setPrettyPrinting().disableHtmlEscaping().create().toJson(out));
      } finally {
        browser.close();
      }
    }
  }
}
