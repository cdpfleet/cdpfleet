// Maven: com.microsoft.playwright:playwright:1.60.0, com.google.code.gson:gson:2.11.0
// Run with PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1.
// env: CDPFLEET_API_KEY, LAB_SITES (comma-separated URLs of the pages to test; one load per page per setup, nothing clicked)
import com.google.gson.*;
import com.microsoft.playwright.*;
import com.microsoft.playwright.options.WaitUntilState;
import java.net.URI;
import java.net.http.*;
import java.security.SecureRandom;
import java.time.Instant;
import java.util.*;
import java.util.concurrent.*;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");
  static final Gson GSON = new GsonBuilder().setPrettyPrinting().disableHtmlEscaping().serializeNulls().create();
  static final HttpClient HTTP = HttpClient.newHttpClient();
  static final String RUN = String.format("%06x", new SecureRandom().nextInt(1 << 24));

  record Setup(String id, String name, String engine, String type, Object headless) {}

  // Every browser the fleet offers that a Playwright 1.60 client can drive (Rebrowser needs 1.52, so it's left out).
  static final List<Setup> SETUPS = List.of(
      new Setup("chrome_headful", "Chrome, headful", "chrome", "chromium", false),
      new Setup("chrome_headless", "Chrome, headless", "chrome", "chromium", "new"),
      new Setup("edge", "Edge, headful", "edge", "chromium", false),
      new Setup("brave", "Brave, headful", "brave", "chromium", false),
      new Setup("opera", "Opera, headful", "opera", "chromium", false),
      new Setup("yandex", "Yandex, headful", "yandex", "chromium", false),
      new Setup("whale", "Whale, headful", "whale", "chromium", false),
      new Setup("chromium_headless", "Chromium, headless", "chromium", "chromium", true),
      new Setup("patchright", "Patchright, headful", "patchright", "chromium", false),
      new Setup("firefox", "Firefox, headful", "firefox", "firefox", false),
      new Setup("webkit", "WebKit, headful", "webkit", "webkit", false),
      new Setup("camoufox", "Camoufox, headful", "camoufox", "firefox", false));

  // What the page shows after it loaded and had 8 s to run its checks, in the lab's words.
  static final String STATE = """
      (() => {
        const t = (document.title + ' ' + (document.body ? document.body.innerText : '').slice(0, 600)).replace(/\\s+/g, ' ');
        const len = document.body ? document.body.innerText.length : 0;
        if (/sorry, you have been blocked|attention required|error 1020|access denied/i.test(t) && len < 4000) return 'blocked';
        if (/just a moment|performing security verification|verify you are human|checking your browser|checking if the site connection is secure/i.test(t) && len < 4000) return 'challenge';
        return 'content';
      })()""";

  static JsonObject api(String method, String path) {
    try {
      HttpRequest req = HttpRequest.newBuilder(URI.create("https://cdpfleet.com" + path)).header("x-api-key", KEY)
          .method(method, HttpRequest.BodyPublishers.noBody()).build();
      return JsonParser.parseString(HTTP.send(req, HttpResponse.BodyHandlers.ofString()).body()).getAsJsonObject();
    } catch (Exception e) {
      return new JsonObject();
    }
  }

  static String launch(String engine, Object headless, String proxy, String profile) throws Exception {
    Map<String, Object> body = new LinkedHashMap<>(Map.of("proxy", proxy, "headless", headless, "record", false));
    if (profile != null) body.put("profile", profile);
    for (int attempt = 1; ; attempt++) {
      HttpRequest req = HttpRequest.newBuilder(URI.create("https://starter.cdpfleet.com/" + engine + "/session"))
          .header("x-api-key", KEY).header("content-type", "application/json")
          .POST(HttpRequest.BodyPublishers.ofString(GSON.toJson(body))).build();
      HttpResponse<String> res = HTTP.send(req, HttpResponse.BodyHandlers.ofString());
      if (res.statusCode() == 200) return JsonParser.parseString(res.body()).getAsJsonObject().get("wsUrl").getAsString();
      if ((res.statusCode() != 429 && res.statusCode() != 503) || attempt == 5) throw new RuntimeException("launch " + res.statusCode() + " " + res.body());
      Thread.sleep(Long.parseLong(res.headers().firstValue("retry-after").orElse("5")) * 1000);
    }
  }

  static Map<String, Object> cell(String outcome, int status) { return new LinkedHashMap<>(Map.of("outcome", outcome, "status", status)); }

  // One session, one load: the status of the page's own response, then its state 8 s later.
  static Map<String, Object> visit(Playwright pw, Setup s, String url, String proxy, String profile) {
    try {
      BrowserType type = switch (s.type()) { case "firefox" -> pw.firefox(); case "webkit" -> pw.webkit(); default -> pw.chromium(); };
      Browser browser = type.connect(launch(s.engine(), s.headless(), proxy, profile),
          new BrowserType.ConnectOptions().setHeaders(Map.of("x-api-key", KEY)));
      try {
        BrowserContext context = profile != null ? browser.contexts().get(0) : browser.newContext();
        Page tab = context.newPage();
        int status;
        try {
          Response res = tab.navigate(url, new Page.NavigateOptions().setWaitUntil(WaitUntilState.DOMCONTENTLOADED).setTimeout(45000));
          status = res != null ? res.status() : 0;
        } catch (PlaywrightException e) {
          return cell("error", 0);
        }
        tab.waitForTimeout(8000);
        String outcome;
        try { outcome = (String) tab.evaluate(STATE); } catch (PlaywrightException e) { outcome = "error"; }
        if ("content".equals(outcome) && status >= 400) outcome = "blocked";
        return cell(outcome, status);
      } finally {
        browser.close();
      }
    } catch (Exception e) {
      return cell("error", 0);
    }
  }

  // One sticky residential exit per site; its setups run one after another (one session at a time per
  // site), and the sites run side by side. A network error gets one more try.
  static Map<String, Object> site(String label, String url) throws Exception {
    try (Playwright pw = Playwright.create()) {
      String proxy = "cdpfleet-resi-country-us-session-c70" + RUN + label + "-lifetime-30";
      Map<String, Object> results = new LinkedHashMap<>();
      for (Setup s : SETUPS) {
        Map<String, Object> r = visit(pw, s, url, proxy, null);
        if ("error".equals(r.get("outcome"))) r = visit(pw, s, url, proxy, null);
        results.put(s.id(), r);
      }
      // A returning visitor: Camoufox with a profile, a first visit and then a second session.
      String name = "case70-" + RUN + "-" + label;
      Setup camoufox = SETUPS.get(SETUPS.size() - 1);
      visit(pw, camoufox, url, proxy, name);
      for (int i = 0; i < 60; i++) {
        boolean free = false;
        JsonObject list = api("GET", "/v1/me/profiles");
        if (list.has("profiles")) for (JsonElement p : list.getAsJsonArray("profiles")) {
          JsonObject o = p.getAsJsonObject();
          if (name.equals(o.get("name").getAsString()) && o.has("in_use") && !o.get("in_use").getAsBoolean()) free = true;
        }
        if (free) break;
        Thread.sleep(2000);
      }
      results.put("camoufox_returning", visit(pw, camoufox, url, proxy, name));
      api("DELETE", "/v1/me/profiles/" + name);
      return new LinkedHashMap<>(Map.of("site", label, "results", results));
    }
  }

  public static void main(String[] args) throws Exception {
    String[] urls = System.getenv("LAB_SITES").split(",");
    ExecutorService pool = Executors.newFixedThreadPool(urls.length);
    List<Future<Map<String, Object>>> futures = new ArrayList<>();
    for (int i = 0; i < urls.length; i++) {
      String label = String.valueOf((char) ('A' + i)), url = urls[i];
      futures.add(pool.submit(() -> site(label, url)));
    }
    List<Map<String, Object>> sites = new ArrayList<>();
    for (Future<Map<String, Object>> f : futures) sites.add(f.get());
    pool.shutdown();
    Map<String, String> names = new LinkedHashMap<>();
    for (Setup s : SETUPS) names.put(s.id(), s.name());
    names.put("camoufox_returning", "Camoufox, returning profile (2nd visit)");
    Map<String, Object> out = new LinkedHashMap<>();
    out.put("measured_at", Instant.now().toString().substring(0, 10));
    out.put("setups", names);
    out.put("sites", sites);
    System.out.println(GSON.toJson(out));
  }
}
