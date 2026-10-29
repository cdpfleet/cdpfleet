// Maven: com.microsoft.playwright:playwright:1.60.0, com.google.code.gson:gson:2.11.0
// Run with PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1.
// env: CDPFLEET_API_KEY, PROXY_URL, COMMUNITY_API_KEY (a Community account: 2 threads, to reach the cap)
// There is no published cdpfleet SDK for Java yet: this is the plain version, with the retry written by hand.
import com.google.gson.*;
import com.microsoft.playwright.*;
import java.net.URI;
import java.net.URLEncoder;
import java.net.http.*;
import java.nio.charset.StandardCharsets;
import java.time.LocalDate;
import java.util.*;
import java.util.concurrent.*;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");
  static final String FREE_KEY = System.getenv("COMMUNITY_API_KEY");
  static final String PROXY = System.getenv("PROXY_URL");
  static final String STARTER = "https://starter.cdpfleet.com";
  static final String API = "https://cdpfleet.com";
  static final HttpClient HTTP = HttpClient.newHttpClient();
  static final Gson GSON = new GsonBuilder().serializeNulls().setPrettyPrinting().disableHtmlEscaping().create();
  static final String[] URLS = {
    "https://books.toscrape.com/catalogue/page-1.html",
    "https://books.toscrape.com/catalogue/page-2.html",
    "https://books.toscrape.com/catalogue/page-3.html",
  };

  // The scrape every variant runs: three catalogue pages, every book title.
  static JsonObject scrape(Browser browser, long start, long connectedMs) {
    Page page = browser.newPage();
    List<String> titles = new ArrayList<>();
    for (String url : URLS) {
      // A shared proxy drops the odd connection: one retry.
      try { page.navigate(url, new Page.NavigateOptions().setTimeout(45000)); }
      catch (PlaywrightException e) { page.navigate(url, new Page.NavigateOptions().setTimeout(45000)); }
      for (ElementHandle a : page.querySelectorAll("article.product_pod h3 a")) titles.add(a.getAttribute("title"));
    }
    JsonObject r = new JsonObject();
    r.addProperty("books", titles.size());
    r.addProperty("first_title", titles.get(0));
    r.addProperty("connected_ms", connectedMs);
    r.addProperty("seconds", (System.currentTimeMillis() - start) / 1000.0);
    return r;
  }

  // ---- 1a. Plain Playwright: launch over HTTP yourself, then connect with the key header.
  static HttpResponse<String> launch(String key, String body) throws Exception {
    return HTTP.send(HttpRequest.newBuilder(URI.create(STARTER + "/chromium/session"))
        .header("x-api-key", key).header("content-type", "application/json")
        .POST(HttpRequest.BodyPublishers.ofString(body)).build(), HttpResponse.BodyHandlers.ofString());
  }

  static JsonObject plain(Playwright pw) throws Exception {
    long t = System.currentTimeMillis();
    HttpResponse<String> res = launch(KEY, "{\"proxy\": " + GSON.toJson(PROXY) + ", \"headless\": true, \"record\": false}");
    if (res.statusCode() != 200) throw new RuntimeException("launch " + res.statusCode() + " " + res.body());
    String wsUrl = JsonParser.parseString(res.body()).getAsJsonObject().get("wsUrl").getAsString();
    Browser browser = pw.chromium().connect(wsUrl, new BrowserType.ConnectOptions().setHeaders(Map.of("x-api-key", KEY)));
    try { return scrape(browser, t, System.currentTimeMillis() - t); }
    finally { browser.close(); } // closing the connection ends the session
  }

  // ---- 1c. A connect URL built by hand: one connect() launches the browser.
  static JsonObject connectUrl(Playwright pw) {
    long t = System.currentTimeMillis();
    String url = "wss://router01.cdpfleet.com/connect/chromium?proxy=" + URLEncoder.encode(PROXY, StandardCharsets.UTF_8) + "&headless=true&record=false";
    Browser browser = pw.chromium().connect(url, new BrowserType.ConnectOptions().setHeaders(Map.of("x-api-key", KEY)));
    try { return scrape(browser, t, System.currentTimeMillis() - t); }
    finally { browser.close(); }
  }

  // ---- 2. At the thread cap (Community: 2 threads). Hold both, then ask for a third.
  static void endSession(String key, String id) {
    try {
      HTTP.send(HttpRequest.newBuilder(URI.create(API + "/v1/me/threads/" + id)).header("x-api-key", key).DELETE().build(), HttpResponse.BodyHandlers.discarding());
    } catch (Exception ignored) { }
  }

  static int liveThreads(String key) throws Exception {
    HttpResponse<String> r = HTTP.send(HttpRequest.newBuilder(URI.create(API + "/v1/me/threads")).header("x-api-key", key).build(), HttpResponse.BodyHandlers.ofString());
    return JsonParser.parseString(r.body()).getAsJsonObject().getAsJsonArray("threads").size();
  }

  // What the SDKs do for you: retry 429 (not the daily/hourly quota) and 503, waiting Retry-After, up to 120 s.
  static JsonObject launchWithRetry(String key, String body) throws Exception {
    long deadline = System.currentTimeMillis() + 120_000;
    for (int attempt = 1; ; attempt++) {
      HttpResponse<String> res = launch(key, body);
      if (res.statusCode() == 200) return JsonParser.parseString(res.body()).getAsJsonObject();
      String code = "";
      try { code = JsonParser.parseString(res.body()).getAsJsonObject().get("error").getAsString(); } catch (Exception ignored) { }
      boolean quota = code.equals("daily_quota_exhausted") || code.equals("hourly_quota_exhausted");
      boolean retryable = res.statusCode() == 503 || (res.statusCode() == 429 && !quota);
      if (!retryable || System.currentTimeMillis() > deadline) throw new RuntimeException("launch " + res.statusCode() + " " + code);
      double after = res.headers().firstValue("retry-after").map(Double::parseDouble).orElse(Math.min(Math.pow(2, attempt - 1), 10));
      Thread.sleep((long) (after * 1000 + Math.random() * 500));
    }
  }

  static JsonObject atTheCap() throws Exception {
    String body = "{\"headless\": true, \"record\": false}";
    List<String> held = new ArrayList<>();
    for (int i = 0; i < 2; i++) {
      HttpResponse<String> r = launch(FREE_KEY, body);
      if (r.statusCode() != 200) throw new RuntimeException("hold " + r.statusCode() + " " + r.body());
      held.add(JsonParser.parseString(r.body()).getAsJsonObject().get("sessionId").getAsString());
      Thread.sleep(1500); // Community launches: at most 2 a second, 12 a minute
    }
    // Plain: the answer comes back at once and the retry is yours to write.
    HttpResponse<String> third = launch(FREE_KEY, body);
    JsonObject tb = JsonParser.parseString(third.body()).getAsJsonObject();
    Thread.sleep(1500);
    // The hand-written loop: free a thread 5 s later and see when it returns.
    String first = held.remove(0);
    ScheduledExecutorService timer = Executors.newSingleThreadScheduledExecutor();
    timer.schedule(() -> endSession(FREE_KEY, first), 5, TimeUnit.SECONDS);
    long t = System.currentTimeMillis();
    JsonObject retry = new JsonObject();
    retry.addProperty("by", "hand-written");
    try {
      JsonObject s = launchWithRetry(FREE_KEY, body);
      retry.addProperty("got_session", true);
      retry.addProperty("waited_s", (System.currentTimeMillis() - t) / 1000.0);
      endSession(FREE_KEY, s.get("sessionId").getAsString()); // never connected: end it explicitly
    } catch (RuntimeException e) {
      retry.addProperty("got_session", false);
      retry.addProperty("waited_s", (System.currentTimeMillis() - t) / 1000.0);
      retry.addProperty("error", e.getMessage());
    }
    timer.shutdown();
    for (String id : held) endSession(FREE_KEY, id);
    Thread.sleep(3000);
    JsonObject plain = new JsonObject();
    plain.addProperty("status", third.statusCode());
    plain.add("error", tb.get("error"));
    plain.addProperty("retry_after", third.headers().firstValue("retry-after").orElse(null));
    plain.add("in_use", tb.get("in_use"));
    plain.add("upgrade_url", tb.has("upgrade") ? tb.getAsJsonObject("upgrade").get("url") : JsonNull.INSTANCE);
    JsonObject cap = new JsonObject();
    cap.addProperty("threads", 2);
    cap.add("plain", plain);
    cap.add("retry", retry);
    cap.addProperty("threads_left", liveThreads(FREE_KEY));
    return cap;
  }

  public static void main(String[] args) throws Exception {
    try (Playwright pw = Playwright.create()) {
      JsonObject out = new JsonObject();
      out.addProperty("language", "java");
      out.add("sdk", JsonNull.INSTANCE);
      JsonObject scrape = new JsonObject();
      scrape.add("plain", plain(pw));
      scrape.add("sdk", JsonNull.INSTANCE);
      scrape.add("connect_url", connectUrl(pw));
      out.add("scrape", scrape);
      out.add("cap", atTheCap());
      out.addProperty("measured_at", LocalDate.now().toString());
      System.out.println(GSON.toJson(out));
    }
  }
}
