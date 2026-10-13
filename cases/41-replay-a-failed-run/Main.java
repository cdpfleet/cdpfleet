// Maven: com.microsoft.playwright:playwright:1.60.0, com.google.code.gson:gson:2.11.0
// Run with PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1. env: CDPFLEET_API_KEY, PROXY_URL
import com.google.gson.*;
import com.microsoft.playwright.*;
import java.net.URI;
import java.net.http.*;
import java.util.*;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");
  static final String SITE = "https://quotes.toscrape.com";
  static final String NEXT = "li.next a"; // the scraper assumes every listing page has a "Next" link
  static final HttpClient HTTP = HttpClient.newHttpClient();

  static double since(long startedMs) { return Math.round((System.currentTimeMillis() - startedMs) / 100.0) / 10.0; }

  static HttpResponse<String> api(String method, String path) throws Exception {
    return HTTP.send(HttpRequest.newBuilder(URI.create("https://cdpfleet.com" + path)).header("x-api-key", KEY)
        .method(method, HttpRequest.BodyPublishers.noBody()).build(), HttpResponse.BodyHandlers.ofString());
  }

  static JsonArray recordings() throws Exception {
    JsonElement list = JsonParser.parseString(api("GET", "/v1/me/recordings").body()).getAsJsonObject().get("recordings");
    return list == null || list.isJsonNull() ? new JsonArray() : list.getAsJsonArray();
  }

  record Session(String id, long started, Browser browser, Page page) {}

  // Headful (the default) sessions are recorded from the browser's display.
  static Session launch(Playwright playwright, Boolean record) throws Exception {
    JsonObject req = new JsonObject();
    req.addProperty("proxy", System.getenv("PROXY_URL"));
    req.addProperty("headless", false);
    if (record != null) req.addProperty("record", record);
    HttpResponse<String> res = HTTP.send(HttpRequest.newBuilder(URI.create("https://starter.cdpfleet.com/chromium/session"))
        .header("x-api-key", KEY).header("content-type", "application/json")
        .POST(HttpRequest.BodyPublishers.ofString(req.toString())).build(), HttpResponse.BodyHandlers.ofString());
    if (res.statusCode() != 200) throw new RuntimeException("launch: " + res.statusCode() + " " + res.body());
    JsonObject body = JsonParser.parseString(res.body()).getAsJsonObject();
    long started = System.currentTimeMillis();
    Browser browser = playwright.chromium().connect(body.get("wsUrl").getAsString(),
        new BrowserType.ConnectOptions().setHeaders(Map.of("x-api-key", KEY)));
    // setViewportSize(null): the page fills the whole display, so it fills the recording too.
    Page page = browser.newContext(new Browser.NewContextOptions().setViewportSize(null)).newPage();
    return new Session(body.get("sessionId").getAsString(), started, browser, page);
  }

  // Proxies drop a tunnel now and then: up to two retries.
  static void go(Page page, String url) throws Exception {
    for (int attempt = 1; ; attempt++) {
      try {
        page.navigate(url, new Page.NavigateOptions().setTimeout(60000));
        return;
      } catch (PlaywrightException e) {
        if (attempt == 3) throw e;
        Thread.sleep(2000);
      }
    }
  }

  public static void main(String[] args) throws Exception {
    JsonObject out = new JsonObject();
    out.addProperty("site", SITE);
    JsonObject scrape = new JsonObject(), recording = new JsonObject(), unrecorded = new JsonObject();
    out.add("scrape", scrape);
    out.add("recording", recording);
    out.add("unrecorded", unrecorded);
    String sessionId;
    try (Playwright playwright = Playwright.create()) {
      // A session launched with record: false, for comparison: it must never get a recording.
      Session quiet = launch(playwright, false);
      try {
        go(quiet.page(), SITE);
      } finally {
        quiet.browser().close();
      }
      unrecorded.addProperty("session_id", quiet.id());

      // The scrape: pages 8, 9, 10 — follow "Next" on each. Page 10 is the last one and has none.
      Session s = launch(playwright, null);
      sessionId = s.id();
      out.addProperty("session_id", sessionId);
      List<String> quotes = new ArrayList<>();
      try {
        go(s.page(), SITE + "/page/8/");
        for (int pageNo = 8; ; pageNo++) {
          quotes.addAll(s.page().locator(".quote .text").allTextContents());
          scrape.addProperty("last_page", pageNo);
          String href;
          try {
            href = s.page().locator(NEXT).getAttribute("href", new Locator.GetAttributeOptions().setTimeout(5000)); // 5 s to find the link
          } catch (PlaywrightException e) {
            scrape.addProperty("failed_at_s", since(s.started()));
            scrape.addProperty("error", (e instanceof TimeoutError ? "timeout" : e.getClass().getSimpleName()) + " waiting for " + NEXT);
            break;
          }
          go(s.page(), URI.create(SITE).resolve(href).toString()); // page loads keep their own timeout and retries
        }
        scrape.addProperty("quotes", quotes.size());
        // What the video shows at that moment: the last page — a "Previous" link and no "Next".
        scrape.addProperty("url_at_failure", s.page().url());
        scrape.addProperty("next_links", s.page().locator("li.next").count());
        scrape.addProperty("previous_links", s.page().locator("li.previous").count());
      } finally {
        s.browser().close(); // ends the session; the node uploads the video afterwards
      }
      out.addProperty("session_seconds", since(s.started()));
    }

    // Find the recording, check it covers the failure, download it, delete it.
    long t = System.currentTimeMillis();
    JsonObject rec = null;
    while (rec == null && System.currentTimeMillis() - t < 600_000) {
      for (JsonElement r : recordings()) {
        if (r.getAsJsonObject().get("session_id").getAsString().equals(sessionId)) rec = r.getAsJsonObject();
      }
      if (rec == null) Thread.sleep(5000);
    }
    if (rec != null) {
      recording.addProperty("appeared_after_s", Math.round((System.currentTimeMillis() - t) / 1000.0));
      String id = rec.get("id").getAsString();
      JsonObject link = JsonParser.parseString(api("GET", "/v1/me/recordings/" + id + "/url").body()).getAsJsonObject();
      HttpResponse<byte[]> video = HTTP.send(HttpRequest.newBuilder(URI.create(link.get("url").getAsString())).build(),
          HttpResponse.BodyHandlers.ofByteArray());
      byte[] bytes = video.body();
      for (String k : List.of("duration_seconds", "width", "height", "bytes")) recording.add(k, rec.get(k));
      recording.addProperty("downloaded_bytes", bytes.length);
      recording.addProperty("content_type", video.headers().firstValue("content-type").orElse(null));
      recording.addProperty("mp4", bytes.length > 8 && new String(bytes, 4, 4).equals("ftyp"));
      recording.add("link_expires_in_s", link.get("expires_in_s"));
      out.addProperty("failure_within_recording", scrape.get("failed_at_s").getAsDouble() <= rec.get("duration_seconds").getAsDouble() + 1);
      recording.addProperty("deleted", api("DELETE", "/v1/me/recordings/" + id).statusCode() == 200);
    }

    // By now the scrape's video has been uploaded; give the unrecorded session another minute.
    Thread.sleep(60_000);
    boolean found = false;
    for (JsonElement r : recordings()) {
      if (r.getAsJsonObject().get("session_id").getAsString().equals(unrecorded.get("session_id").getAsString())) found = true;
    }
    unrecorded.addProperty("recording_found", found);
    System.out.println(new GsonBuilder().setPrettyPrinting().create().toJson(out));
  }
}
