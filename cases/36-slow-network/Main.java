// Maven: com.microsoft.playwright:playwright:1.60.0, com.google.code.gson:gson:2.11.0
// Run with PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1. env: CDPFLEET_API_KEY, PROXY_URL
import com.google.gson.*;
import com.microsoft.playwright.*;
import com.microsoft.playwright.options.LoadState;
import com.microsoft.playwright.options.WaitUntilState;
import java.net.URI;
import java.net.http.*;
import java.util.*;
import java.util.regex.*;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");
  static final String URL = "https://books.toscrape.com/";
  static final Gson GSON = new GsonBuilder().serializeNulls().create();

  // Chrome DevTools' presets (throughput in bytes/s, latency in ms).
  static JsonObject conditions(double latency, double download, double upload) {
    JsonObject c = new JsonObject();
    c.addProperty("offline", false);
    c.addProperty("latency", latency);
    c.addProperty("downloadThroughput", download);
    c.addProperty("uploadThroughput", upload);
    return c;
  }

  public static void main(String[] args) throws Exception {
    Map<String, JsonObject> profiles = new LinkedHashMap<>();
    profiles.put("no throttling", null);
    profiles.put("Fast 4G", conditions(150, 9 * 1024 * 1024 / 8.0, 1.5 * 1024 * 1024 / 8));
    profiles.put("Slow 3G", conditions(2000, 400 * 1024 / 8.0, 400 * 1024 / 8.0));

    String body = "{\"proxy\": " + GSON.toJson(System.getenv("PROXY_URL")) + ", \"headless\": \"new\"}";
    HttpResponse<String> res = HttpClient.newHttpClient().send(HttpRequest.newBuilder(URI.create("https://starter.cdpfleet.com/chromium/session"))
        .header("x-api-key", KEY).header("content-type", "application/json")
        .POST(HttpRequest.BodyPublishers.ofString(body)).build(), HttpResponse.BodyHandlers.ofString());
    if (res.statusCode() != 200) throw new RuntimeException("launch: " + res.statusCode() + " " + res.body());
    String wsUrl = JsonParser.parseString(res.body()).getAsJsonObject().get("wsUrl").getAsString();

    try (Playwright playwright = Playwright.create()) {
      Browser browser = playwright.chromium().connect(wsUrl, new BrowserType.ConnectOptions().setHeaders(Map.of("x-api-key", KEY)));
      try {
        JsonArray rows = new JsonArray();
        for (Map.Entry<String, JsonObject> profile : profiles.entrySet()) {
          BrowserContext context = browser.newContext(); // no cache between runs
          Page page = context.newPage();
          // A CDP session works over the normal Playwright connection on Chromium-based browsers.
          CDPSession cdp = context.newCDPSession(page);
          cdp.send("Network.enable");
          if (profile.getValue() != null) cdp.send("Network.emulateNetworkConditions", profile.getValue());
          long t = System.currentTimeMillis();
          page.navigate(URL, new Page.NavigateOptions().setWaitUntil(WaitUntilState.DOMCONTENTLOADED).setTimeout(120000));
          long htmlMs = System.currentTimeMillis() - t;
          page.waitForLoadState(LoadState.LOAD, new Page.WaitForLoadStateOptions().setTimeout(180000));
          JsonObject row = new JsonObject();
          row.addProperty("profile", profile.getKey());
          row.addProperty("html_ms", htmlMs);
          row.addProperty("load_ms", System.currentTimeMillis() - t);
          rows.add(row);
          context.close();
        }

        // Offline: the page fails like it would on a dead connection.
        BrowserContext context = browser.newContext();
        Page page = context.newPage();
        CDPSession cdp = context.newCDPSession(page);
        cdp.send("Network.enable");
        JsonObject off = new JsonObject();
        off.addProperty("offline", true);
        off.addProperty("latency", 0);
        off.addProperty("downloadThroughput", -1);
        off.addProperty("uploadThroughput", -1);
        cdp.send("Network.emulateNetworkConditions", off);
        String offline;
        try {
          page.navigate(URL, new Page.NavigateOptions().setTimeout(30000));
          offline = "loaded";
        } catch (PlaywrightException e) {
          Matcher m = Pattern.compile("net::[A-Z_]+").matcher(e.getMessage());
          offline = m.find() ? m.group() : e.getMessage().split("\n")[0];
        }
        context.close();

        JsonObject out = new JsonObject();
        out.add("rows", rows);
        out.addProperty("offline", offline);
        System.out.println(new GsonBuilder().serializeNulls().setPrettyPrinting().disableHtmlEscaping().create().toJson(out));
      } finally {
        browser.close();
      }
    }
  }
}
