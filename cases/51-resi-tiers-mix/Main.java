// Maven: com.microsoft.playwright:playwright:1.60.0, com.google.code.gson:gson:2.11.0
// Run with PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1.
// env: CDPFLEET_API_KEY (no proxy of your own needed: the cdpfleet residential proxy, billed per GB by tier)
import com.google.gson.*;
import com.microsoft.playwright.*;
import com.microsoft.playwright.options.*;
import java.net.URI;
import java.net.http.*;
import java.util.*;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");
  static final String PAGE = "https://commons.wikimedia.org/wiki/Category:Cats";
  static final Map<Integer, Double> PRICE = Map.of(1, 5.0, 4, 0.5); // USD per GB (10^9 bytes)
  static final List<String> CDN = List.of("thumb.wikimedia.org", "upload.wikimedia.org"); // the images; HTML, CSS and JS come from commons.wikimedia.org
  static final Gson GSON = new GsonBuilder().setPrettyPrinting().disableHtmlEscaping().create();
  static final HttpClient HTTP = HttpClient.newHttpClient();

  // One session, two prices: the page through Tier 1, the image hosts through Tier 4.
  static JsonObject launchBody() {
    JsonObject rule = new JsonObject();
    rule.add("hosts", GSON.toJsonTree(CDN));
    rule.addProperty("proxy", "cdpfleet-resi-tier-4-country-us");
    JsonArray rules = new JsonArray();
    rules.add(rule);
    JsonObject body = new JsonObject();
    body.addProperty("headless", "new");
    body.addProperty("record", false);
    body.addProperty("proxy", "cdpfleet-resi-country-us");
    body.add("proxy_rules", rules);
    return body;
  }

  // Today's residential traffic per tier, as billed.
  static Map<Integer, Long> usedToday() throws Exception {
    HttpResponse<String> res = HTTP.send(HttpRequest.newBuilder(URI.create("https://cdpfleet.com/v1/me/proxy-balance"))
        .header("x-api-key", KEY).GET().build(), HttpResponse.BodyHandlers.ofString());
    Map<Integer, Long> used = new HashMap<>();
    for (JsonElement t : JsonParser.parseString(res.body()).getAsJsonObject().getAsJsonArray("used_today")) {
      used.put(t.getAsJsonObject().get("tier").getAsInt(), t.getAsJsonObject().get("bytes").getAsLong());
    }
    return used;
  }

  // Load the gallery, scroll to the end, and add up every request's bytes by host group.
  static JsonObject load(Playwright playwright) throws Exception {
    HttpResponse<String> res = HTTP.send(HttpRequest.newBuilder(URI.create("https://starter.cdpfleet.com/chromium/session"))
        .header("x-api-key", KEY).header("content-type", "application/json")
        .POST(HttpRequest.BodyPublishers.ofString(launchBody().toString())).build(), HttpResponse.BodyHandlers.ofString());
    if (res.statusCode() != 200) throw new RuntimeException("launch " + res.statusCode() + " " + res.body());
    String wsUrl = JsonParser.parseString(res.body()).getAsJsonObject().get("wsUrl").getAsString();
    Browser browser = playwright.chromium().connect(wsUrl, new BrowserType.ConnectOptions().setHeaders(Map.of("x-api-key", KEY)));
    try {
      Page page = browser.newPage();
      List<Request> done = new ArrayList<>();
      page.onRequestFinished(done::add);
      // Residential peers drop the odd connection: retry the navigation once.
      for (int attempt = 1; ; attempt++) {
        try {
          page.navigate(PAGE, new Page.NavigateOptions().setWaitUntil(WaitUntilState.LOAD).setTimeout(60000));
          break;
        } catch (PlaywrightException e) {
          if (attempt == 2) throw e;
        }
      }
      for (int i = 0; i < 20; i++) { // lazy-loaded thumbnails load as they scroll into view
        page.mouse().wheel(0, 1500);
        page.waitForTimeout(250);
      }
      try {
        page.waitForLoadState(LoadState.NETWORKIDLE, new Page.WaitForLoadStateOptions().setTimeout(15000));
      } catch (PlaywrightException ignored) {
      }
      long pageBytes = 0, imageBytes = 0;
      int images = 0;
      for (Request req : new ArrayList<>(done)) {
        Sizes s;
        try {
          s = req.sizes();
        } catch (PlaywrightException e) {
          continue;
        }
        long total = s.requestHeadersSize + s.requestBodySize + s.responseHeadersSize + s.responseBodySize;
        if (CDN.contains(URI.create(req.url()).getHost())) {
          imageBytes += total;
          images++;
        } else {
          pageBytes += total;
        }
      }
      JsonObject run = new JsonObject();
      run.addProperty("requests", done.size());
      run.addProperty("image_requests", images);
      run.addProperty("page_kb", Math.round(pageBytes / 1000.0));
      run.addProperty("image_kb", Math.round(imageBytes / 1000.0));
      return run;
    } finally {
      browser.close();
    }
  }

  public static void main(String[] args) throws Exception {
    Map<Integer, Long> before = usedToday();
    JsonObject run;
    try (Playwright playwright = Playwright.create()) {
      run = load(playwright);
    }
    long pageKb = run.get("page_kb").getAsLong();
    long imageKb = run.get("image_kb").getAsLong();

    // Metering trails the traffic by about a minute: wait until Tier 4 shows the images.
    Map<Integer, Long> after = before;
    for (int i = 0; i < 18 && (after.getOrDefault(4, 0L) - before.getOrDefault(4, 0L)) / 1000.0 < imageKb * 0.8; i++) {
      Thread.sleep(10000);
      after = usedToday();
    }

    // What 1,000 such loads cost: everything on Tier 1 versus the split.
    JsonObject cost = new JsonObject();
    cost.addProperty("all_tier_1", Math.round((pageKb * PRICE.get(1) + imageKb * PRICE.get(1)) / 1e6 * 1000 * 100) / 100.0);
    cost.addProperty("page_tier_1_images_tier_4", Math.round((pageKb * PRICE.get(1) + imageKb * PRICE.get(4)) / 1e6 * 1000 * 100) / 100.0);
    JsonObject billed = new JsonObject();
    billed.addProperty("tier_1", Math.round((after.getOrDefault(1, 0L) - before.getOrDefault(1, 0L)) / 1000.0));
    billed.addProperty("tier_4", Math.round((after.getOrDefault(4, 0L) - before.getOrDefault(4, 0L)) / 1000.0));

    JsonObject doc = new JsonObject();
    doc.addProperty("page", PAGE);
    doc.add("run", run);
    doc.addProperty("image_share", Math.round((double) imageKb / (imageKb + pageKb) * 100) / 100.0);
    doc.add("cost_per_1000_loads_usd", cost);
    doc.add("billed_kb", billed);
    System.out.println(GSON.toJson(doc));
  }
}
