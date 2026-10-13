// Maven: com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0
// env: CDPFLEET_API_KEY, PROXY_URL
// Java has no maintained Puppeteer, so this is Playwright over CDP: connectOverCDP(cdpUrl)
// plus raw CDP sessions for the same browser- and page-level commands Puppeteer sends.
import com.google.gson.*;
import com.microsoft.playwright.*;
import com.microsoft.playwright.options.WaitUntilState;
import java.net.URI;
import java.net.http.*;
import java.util.*;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");
  static final String URL = "https://books.toscrape.com/";
  static final HttpClient HTTP = HttpClient.newHttpClient();
  // Bytes the page pulled over the network, from its own resource timing (fresh context: no cache).
  static final String TRANSFER = "() => {"
      + " const all = [...performance.getEntriesByType('navigation'), ...performance.getEntriesByType('resource')];"
      + " return { resources: all.length, transfer_kb: Math.round(all.reduce((n, e) => n + (e.transferSize || 0), 0) / 1024) }; }";

  static boolean isLive(String sessionId) throws Exception {
    HttpResponse<String> r = HTTP.send(HttpRequest.newBuilder(URI.create("https://cdpfleet.com/v1/me/threads")).header("x-api-key", KEY).build(),
        HttpResponse.BodyHandlers.ofString());
    return r.body().contains(sessionId);
  }

  static void gotoWithRetry(Page page) {
    for (int attempt = 1; ; attempt++) { // the proxy can drop a tunnel; retry
      try {
        page.navigate(URL, new Page.NavigateOptions().setWaitUntil(WaitUntilState.LOAD).setTimeout(60000));
        return;
      } catch (PlaywrightException e) {
        if (attempt == 3) throw e;
      }
    }
  }

  record Load(BrowserContext context, Page page, JsonObject row) {}

  // One page in a fresh browser context, optionally aborting image requests.
  static Load load(Browser browser, boolean blockImages) {
    BrowserContext context = browser.newContext();
    Page page = context.newPage();
    int[] blocked = {0};
    if (blockImages) {
      page.route("**/*", route -> {
        if (route.request().resourceType().equals("image")) { blocked[0]++; route.abort(); } else route.resume();
      });
    }
    long t = System.currentTimeMillis();
    gotoWithRetry(page);
    long loadMs = System.currentTimeMillis() - t;
    JsonObject transfer = new Gson().toJsonTree(page.evaluate(TRANSFER)).getAsJsonObject();
    JsonObject row = new JsonObject();
    row.addProperty("label", blockImages ? "images blocked" : "everything");
    row.addProperty("resources", transfer.get("resources").getAsInt());
    row.addProperty("transfer_kb", transfer.get("transfer_kb").getAsInt());
    row.addProperty("images_blocked", blocked[0]);
    row.addProperty("load_ms", loadMs);
    return new Load(context, page, row);
  }

  public static void main(String[] args) throws Exception {
    JsonObject body = new JsonObject();
    body.addProperty("proxy", System.getenv("PROXY_URL"));
    body.addProperty("headless", "new");
    body.addProperty("cdp", true);
    body.addProperty("record", false);
    HttpResponse<String> res = HTTP.send(HttpRequest.newBuilder(URI.create("https://starter.cdpfleet.com/chrome/session"))
        .header("x-api-key", KEY).header("content-type", "application/json")
        .POST(HttpRequest.BodyPublishers.ofString(body.toString())).build(), HttpResponse.BodyHandlers.ofString());
    if (res.statusCode() != 200) throw new RuntimeException("launch " + res.statusCode() + " " + res.body());
    JsonObject s = JsonParser.parseString(res.body()).getAsJsonObject();
    String sessionId = s.get("sessionId").getAsString();

    JsonObject out = new JsonObject();
    out.addProperty("client", "Playwright 1.60 connectOverCDP");
    try (Playwright playwright = Playwright.create()) {
      long t = System.currentTimeMillis();
      Browser browser = playwright.chromium().connectOverCDP(s.get("cdpUrl").getAsString(),
          new BrowserType.ConnectOverCDPOptions().setHeaders(Map.of("x-api-key", KEY)));
      out.addProperty("connect_ms", System.currentTimeMillis() - t);
      try {
        // Raw CDP at the browser level: what's really on the other end.
        CDPSession cdp = browser.newBrowserCDPSession();
        JsonObject version = cdp.send("Browser.getVersion");
        out.addProperty("product", version.get("product").getAsString());
        out.addProperty("protocol_version", version.get("protocolVersion").getAsString());
        int before = pageCount(cdp);
        Load full = load(browser, false);
        Load lean = load(browser, true);
        JsonObject targets = new JsonObject();
        targets.addProperty("pages_before", before);
        targets.addProperty("pages_after", pageCount(cdp));
        out.add("targets", targets);
        JsonArray loads = new JsonArray();
        loads.add(full.row());
        loads.add(lean.row());
        out.add("loads", loads);

        // Performance.getMetrics over a page-level CDP session (Puppeteer's page.metrics()).
        CDPSession pageCdp = full.context().newCDPSession(full.page());
        pageCdp.send("Performance.enable");
        Map<String, Double> m = new HashMap<>();
        for (JsonElement e : pageCdp.send("Performance.getMetrics").getAsJsonArray("metrics")) {
          m.put(e.getAsJsonObject().get("name").getAsString(), e.getAsJsonObject().get("value").getAsDouble());
        }
        JsonObject metrics = new JsonObject();
        metrics.addProperty("dom_nodes", m.get("Nodes").intValue());
        metrics.addProperty("js_heap_used_mb", Math.round(m.get("JSHeapUsedSize") / 1048576 * 10) / 10.0);
        out.add("metrics", metrics);
        full.context().close();
        lean.context().close();
      } finally {
        browser.close(); // closes the connection, which ends the cdpfleet session
      }
    }
    long closed = System.currentTimeMillis();
    while (isLive(sessionId) && System.currentTimeMillis() - closed < 60000) Thread.sleep(1000);
    if (isLive(sessionId)) out.add("session_ended_s", JsonNull.INSTANCE);
    else out.addProperty("session_ended_s", Math.round((System.currentTimeMillis() - closed) / 100.0) / 10.0);
    System.out.println(new GsonBuilder().setPrettyPrinting().serializeNulls().create().toJson(out));
  }

  static int pageCount(CDPSession cdp) {
    int n = 0;
    for (JsonElement e : cdp.send("Target.getTargets").getAsJsonArray("targetInfos")) {
      if (e.getAsJsonObject().get("type").getAsString().equals("page")) n++;
    }
    return n;
  }
}
