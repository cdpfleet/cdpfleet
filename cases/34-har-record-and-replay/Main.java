// Maven: com.microsoft.playwright:playwright:1.60.0, com.google.code.gson:gson:2.11.0
// Run with PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1. env: CDPFLEET_API_KEY, PROXY_URL
import com.google.gson.*;
import com.microsoft.playwright.*;
import com.microsoft.playwright.options.*;
import java.net.URI;
import java.net.http.*;
import java.nio.file.*;
import java.util.*;
import java.util.regex.Pattern;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");
  static final String URL = "https://books.toscrape.com/";
  static final Gson GSON = new GsonBuilder().serializeNulls().create();

  public static void main(String[] args) throws Exception {
    String body = "{\"proxy\": " + GSON.toJson(System.getenv("PROXY_URL")) + ", \"headless\": \"new\"}";
    HttpResponse<String> res = HttpClient.newHttpClient().send(HttpRequest.newBuilder(URI.create("https://starter.cdpfleet.com/chromium/session"))
        .header("x-api-key", KEY).header("content-type", "application/json")
        .POST(HttpRequest.BodyPublishers.ofString(body)).build(), HttpResponse.BodyHandlers.ofString());
    if (res.statusCode() != 200) throw new RuntimeException("launch: " + res.statusCode() + " " + res.body());
    String wsUrl = JsonParser.parseString(res.body()).getAsJsonObject().get("wsUrl").getAsString();

    try (Playwright playwright = Playwright.create()) {
      Browser browser = playwright.chromium().connect(wsUrl, new BrowserType.ConnectOptions().setHeaders(Map.of("x-api-key", KEY)));
      Path dir = Files.createTempDirectory("har-");
      Path har = dir.resolve("books.har");
      try {
        // 1. Record: every request of the page goes into a HAR file on this machine.
        BrowserContext rec = browser.newContext(new Browser.NewContextOptions().setRecordHarPath(har).setRecordHarMode(HarMode.MINIMAL));
        Page p1 = rec.newPage();
        long t = System.currentTimeMillis();
        p1.navigate(URL, new Page.NavigateOptions().setWaitUntil(WaitUntilState.LOAD).setTimeout(60000));
        long liveMs = System.currentTimeMillis() - t;
        int liveBooks = p1.locator("article.product_pod").count();
        rec.close(); // writes the HAR
        JsonArray entries = JsonParser.parseString(Files.readString(har)).getAsJsonObject().getAsJsonObject("log").getAsJsonArray("entries");

        // 2. Replay: serve every request from the HAR; anything not in it is refused, so the
        //    page can't quietly reach the network.
        BrowserContext rep = browser.newContext();
        int[] fromHar = {0};
        int[] refused = {0};
        rep.onRequestFinished(r -> fromHar[0]++);
        rep.onRequestFailed(r -> refused[0]++);
        rep.routeFromHAR(har, new BrowserContext.RouteFromHAROptions().setNotFound(HarNotFound.ABORT));
        Page p2 = rep.newPage();
        t = System.currentTimeMillis();
        p2.navigate(URL, new Page.NavigateOptions().setWaitUntil(WaitUntilState.LOAD).setTimeout(60000));
        long replayMs = System.currentTimeMillis() - t;
        int replayBooks = p2.locator("article.product_pod").count();
        // A page the HAR doesn't have: refused instead of fetched.
        String missing;
        try {
          p2.navigate("https://books.toscrape.com/catalogue/page-2.html");
          missing = "loaded";
        } catch (PlaywrightException e) {
          String msg = String.valueOf(e.getMessage());
          missing = Pattern.compile("ERR_FAILED|aborted", Pattern.CASE_INSENSITIVE).matcher(msg).find() ? "refused" : msg.split("\n")[0];
        }
        rep.close();

        JsonObject live = new JsonObject();
        live.addProperty("ms", liveMs);
        live.addProperty("books", liveBooks);
        JsonObject replay = new JsonObject();
        replay.addProperty("ms", replayMs);
        replay.addProperty("books", replayBooks);
        replay.addProperty("served_from_har", fromHar[0]);
        replay.addProperty("refused", refused[0]);
        JsonObject out = new JsonObject();
        out.addProperty("har_entries", entries.size());
        out.addProperty("har_kb", Math.round(Files.size(har) / 1024.0));
        out.add("live", live);
        out.add("replay", replay);
        out.addProperty("page_not_in_har", missing);

        System.out.println(new GsonBuilder().serializeNulls().setPrettyPrinting().disableHtmlEscaping().create().toJson(out));
      } finally {
        browser.close();
        try (var walk = Files.walk(dir)) {
          walk.sorted(Comparator.reverseOrder()).forEach(f -> f.toFile().delete());
        }
      }
    }
  }
}
