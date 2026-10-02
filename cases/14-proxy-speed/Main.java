// Maven: com.microsoft.playwright:playwright:1.60.0, com.google.code.gson:gson:2.11.0
// Run with PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1.
// env: CDPFLEET_API_KEY, PROXY_URL, PROXY_URL_DE, SOCKS_PROXIES (comma-separated socks5:// URLs)
import com.google.gson.*;
import com.microsoft.playwright.*;
import com.microsoft.playwright.options.*;
import java.net.URI;
import java.net.http.*;
import java.util.*;
import java.util.concurrent.*;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");
  static final String PAGE = "https://en.wikipedia.org/wiki/Web_browser";
  static final int LOADS = 3; // per proxy, each in a fresh context (cold cache, new connections)

  // Navigation Timing + Largest Contentful Paint, read in the page after load.
  static final String TIMINGS = """
      () => new Promise((done) => {
        const nav = performance.getEntriesByType('navigation')[0];
        new PerformanceObserver((list) => {
          const lcp = list.getEntries().at(-1);
          done({
            connect: nav.connectEnd - nav.connectStart,
            ttfb: nav.responseStart - nav.requestStart,
            domReady: nav.domContentLoadedEventEnd,
            load: nav.loadEventEnd,
            lcp: lcp.startTime,
          });
        }).observe({ type: 'largest-contentful-paint', buffered: true });
      })""";

  static JsonObject measure(String label, String proxy) throws Exception {
    String body = "{\"proxy\": " + new Gson().toJson(proxy) + ", \"headless\": true}";
    HttpResponse<String> res = HttpClient.newHttpClient().send(HttpRequest.newBuilder(URI.create("https://starter.cdpfleet.com/chromium/session"))
        .header("x-api-key", KEY).header("content-type", "application/json")
        .POST(HttpRequest.BodyPublishers.ofString(body)).build(), HttpResponse.BodyHandlers.ofString());
    JsonObject out = new JsonObject();
    out.addProperty("proxy", label);
    if (res.statusCode() != 200) { out.addProperty("error", "launch " + res.statusCode()); return out; }
    String wsUrl = JsonParser.parseString(res.body()).getAsJsonObject().get("wsUrl").getAsString();
    try (Playwright playwright = Playwright.create()) { // Playwright objects are per thread
      Browser browser = playwright.chromium().connect(wsUrl, new BrowserType.ConnectOptions().setHeaders(Map.of("x-api-key", KEY)));
      try {
        List<JsonObject> runs = new ArrayList<>();
        int failures = 0;
        while (runs.size() < LOADS) {
          BrowserContext context = browser.newContext();
          try {
            Page page = context.newPage();
            page.navigate(PAGE, new Page.NavigateOptions().setWaitUntil(WaitUntilState.LOAD).setTimeout(60000));
            runs.add(new Gson().toJsonTree(page.evaluate(TIMINGS)).getAsJsonObject());
          } catch (PlaywrightException err) {
            if (++failures > 1) throw err; // one failed load is the proxy's noise; two is a problem
          } finally {
            context.close();
          }
        }
        out.addProperty("loads", runs.size());
        for (String[] k : new String[][] {{"connect", "connect_ms"}, {"ttfb", "ttfb_ms"}, {"domReady", "dom_ready_ms"}, {"lcp", "lcp_ms"}, {"load", "load_ms"}}) {
          double[] v = runs.stream().mapToDouble(r -> r.get(k[0]).getAsDouble()).sorted().toArray();
          out.addProperty(k[1], Math.round(v[v.length / 2])); // median
        }
        return out;
      } finally {
        browser.close();
      }
    }
  }

  public static void main(String[] args) throws Exception {
    Map<String, String> proxies = new LinkedHashMap<>();
    proxies.put("residential (any country)", System.getenv("PROXY_URL"));
    proxies.put("residential (Germany)", System.getenv("PROXY_URL_DE"));
    proxies.put("datacenter SOCKS5", System.getenv("SOCKS_PROXIES").split(",")[0]);
    ExecutorService pool = Executors.newFixedThreadPool(proxies.size());
    List<Future<JsonObject>> jobs = new ArrayList<>();
    for (Map.Entry<String, String> e : proxies.entrySet()) jobs.add(pool.submit(() -> measure(e.getKey(), e.getValue())));
    JsonArray rows = new JsonArray();
    for (Future<JsonObject> f : jobs) rows.add(f.get());
    pool.shutdown();
    JsonObject out = new JsonObject();
    out.addProperty("page", PAGE);
    out.addProperty("statistic", "median of " + LOADS + " cold loads");
    out.add("results", rows);
    System.out.println(new GsonBuilder().setPrettyPrinting().disableHtmlEscaping().create().toJson(out));
  }
}
