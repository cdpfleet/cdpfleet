// Maven: com.microsoft.playwright:playwright:1.60.0, com.google.code.gson:gson:2.11.0
// Run with PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1. env: CDPFLEET_API_KEY, PROXY_URL_US
import com.google.gson.*;
import com.microsoft.playwright.*;
import com.microsoft.playwright.options.*;
import java.net.URI;
import java.net.http.*;
import java.util.*;
import java.util.regex.*;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");
  static final String[][] SITES = {
      {"The Guardian", "https://www.theguardian.com/international"},
      {"NPR", "https://www.npr.org/"},
      {"Wikipedia", "https://en.wikipedia.org/wiki/Web_browser"}};
  // Analytics, measurement and real-user-monitoring hosts (subdomains included) — not ads.
  static final List<String> TELEMETRY = List.of("google-analytics.com", "analytics.google.com", "googletagmanager.com", "ophan.theguardian.com",
      "performance-events.guardianapis.com", "feast-events.guardianapis.com", "scorecardresearch.com", "permutive.app",
      "permutive.com", "go-mpulse.net", "akstat.io", "nr-data.net", "newrelic.com", "sentry.io", "datadoghq.com",
      "hotjar.com", "clarity.ms", "segment.io", "segment.com", "chartbeat.com", "chartbeat.net", "parsely.com",
      "analytics.yahoo.com", "omtrdc.net", "demdex.net", "bat.bing.com", "cloudflareinsights.com", "brandmetrics.com");

  static final Pattern HOST = Pattern.compile("^[a-z]+://([^/:?#]+)");

  // Telemetry: one of those hosts, or a beacon (navigator.sendBeacon, <a ping>).
  static boolean isTelemetry(Request req) {
    if (req.resourceType().equals("ping")) return true;
    // A lenient host match: URI.create() rejects the unencoded { } some pages put in query strings.
    Matcher u = HOST.matcher(req.url());
    if (!u.find()) return false;
    String host = u.group(1).toLowerCase();
    return TELEMETRY.stream().anyMatch(d -> host.equals(d) || host.endsWith("." + d));
  }

  // One load in a fresh context: DOM ready + 6 s, then leave the page (about:blank) and give the
  // page-hide beacons 1.5 s. Counts every request, the telemetry among them, and bytes on the wire.
  static Map<String, Object> load(Browser browser, String url, boolean block) {
    BrowserContext context = browser.newContext();
    long[] m = new long[6]; // requests, telemetry, on leave, bytes, telemetry bytes, blocked
    boolean[] leaving = {false};
    if (block) {
      context.route("**/*", route -> {
        if (isTelemetry(route.request())) { m[5]++; route.abort(); } else route.resume();
      });
    }
    Page page = context.newPage();
    page.onRequest(req -> {
      m[0]++;
      if (isTelemetry(req)) { m[1]++; if (leaving[0]) m[2]++; }
    });
    page.onRequestFinished(req -> {
      Sizes s;
      try { s = req.sizes(); } catch (PlaywrightException e) { return; }
      long n = s.requestHeadersSize + s.requestBodySize + s.responseHeadersSize + s.responseBodySize;
      m[3] += n;
      if (isTelemetry(req)) m[4] += n;
    });
    long t = System.currentTimeMillis();
    page.navigate(url, new Page.NavigateOptions().setWaitUntil(WaitUntilState.DOMCONTENTLOADED).setTimeout(90000));
    long domReady = System.currentTimeMillis() - t;
    page.waitForTimeout(6000);
    String title = page.title();
    int headings = page.locator("h1, h2, h3").count(); // the content is still there
    leaving[0] = true;
    page.navigate("about:blank");
    page.waitForTimeout(1500);
    context.close();
    Map<String, Object> r = new HashMap<>();
    r.put("requests", m[0]); r.put("telemetry", m[1]); r.put("on_leave", m[2]); r.put("bytes", m[3]); r.put("telemetry_bytes", m[4]);
    r.put("blocked", m[5]); r.put("dom_ready", domReady); r.put("title", title); r.put("headings", headings);
    return r;
  }

  public static void main(String[] args) throws Exception {
    String body = "{\"proxy\": " + new Gson().toJson(System.getenv("PROXY_URL_US")) + ", \"headless\": true}";
    HttpResponse<String> res = HttpClient.newHttpClient().send(HttpRequest.newBuilder(URI.create("https://starter.cdpfleet.com/chromium/session"))
        .header("x-api-key", KEY).header("content-type", "application/json")
        .POST(HttpRequest.BodyPublishers.ofString(body)).build(), HttpResponse.BodyHandlers.ofString());
    if (res.statusCode() != 200) throw new RuntimeException("launch: " + res.statusCode() + " " + res.body());
    String wsUrl = JsonParser.parseString(res.body()).getAsJsonObject().get("wsUrl").getAsString();

    try (Playwright playwright = Playwright.create()) {
      Browser browser = playwright.chromium().connect(wsUrl, new BrowserType.ConnectOptions().setHeaders(Map.of("x-api-key", KEY)));
      try {
        JsonArray out = new JsonArray();
        for (String[] site : SITES) {
          Map<String, Object> off = load(browser, site[1], false);
          Map<String, Object> on = load(browser, site[1], true);
          String title = (String) off.get("title");
          JsonObject row = new JsonObject();
          row.addProperty("site", site[0]);
          row.addProperty("title_ok", !title.isEmpty() && title.equals(on.get("title")));
          row.addProperty("requests", (Long) off.get("requests"));
          row.addProperty("telemetry_requests", (Long) off.get("telemetry"));
          row.addProperty("telemetry_on_leave", (Long) off.get("on_leave"));
          row.addProperty("telemetry_kb", Math.round((Long) off.get("telemetry_bytes") / 1024.0));
          row.addProperty("total_kb", Math.round((Long) off.get("bytes") / 1024.0));
          row.addProperty("blocked", (Long) on.get("blocked"));
          row.addProperty("headings", (Integer) off.get("headings"));
          row.addProperty("headings_blocked_run", (Integer) on.get("headings"));
          row.addProperty("dom_ready_ms", (Long) off.get("dom_ready"));
          row.addProperty("dom_ready_ms_blocked_run", (Long) on.get("dom_ready"));
          out.add(row);
        }
        System.out.println(new GsonBuilder().setPrettyPrinting().disableHtmlEscaping().create().toJson(out));
      } finally {
        browser.close();
      }
    }
  }
}
