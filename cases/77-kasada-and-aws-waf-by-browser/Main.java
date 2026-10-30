// Maven: com.microsoft.playwright:playwright:1.60.0, com.google.code.gson:gson:2.11.0
// Run with PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1.
// env: CDPFLEET_API_KEY, LAB_SITES_KASADA_AWSWAF (comma-separated URLs of the pages to test; one load per page per setup, nothing clicked)
import com.google.gson.*;
import com.microsoft.playwright.*;
import com.microsoft.playwright.options.WaitUntilState;
import java.net.URI;
import java.net.http.*;
import java.security.SecureRandom;
import java.time.Instant;
import java.util.*;
import java.util.concurrent.*;
import java.util.regex.Pattern;

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

  // What the page shows after it loaded and had 8 s to run its checks, in the lab's words. AWS WAF's
  // CAPTCHA ("Let's confirm you are human") is interactive: a challenge. A near-empty page 8 s later
  // (Kasada's 429 answer is a blank page with a script) or an "access denied" page is blocked.
  static final String STATE = """
      (() => {
        const text = document.body ? document.body.innerText : '';
        const t = (document.title + ' ' + text.slice(0, 600)).replace(/\\s+/g, ' ');
        if (document.querySelector('#captcha-container, awswaf-captcha') || (/confirm you are human|verify you are (a )?human|are you a robot|solve the puzzle/i.test(t) && text.length < 4000)) return 'challenge';
        if (/access (to this page has been )?denied|has been blocked|you don.t have permission to access|request (unsuccessful|blocked)|too many requests/i.test(t) && text.length < 4000) return 'blocked';
        if (text.trim().length < 200) return 'blank';
        return 'content';
      })()""";

  static final Pattern KASADA_URL = Pattern.compile("/ips\\.js(\\?|$)|/(tl|fp)(\\?|$)");
  static final Pattern AWS_URL = Pattern.compile("awswaf\\.com|/challenge\\.js(\\?|$)");
  static final Pattern VENDOR_COOKIE = Pattern.compile("(?i)^(KP_|aws-waf)");

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

  static Map<String, Object> failed(String outcome) {
    Map<String, Object> m = new LinkedHashMap<>();
    m.put("outcome", outcome);
    m.put("status", 0);
    m.put("final_status", 0);
    m.put("kasada", false);
    m.put("aws_waf", false);
    m.put("cookies", List.of());
    return m;
  }

  static boolean anyPrefix(Map<String, String> headers, String p) {
    for (String n : headers.keySet()) if (n.toLowerCase().startsWith(p)) return true;
    return false;
  }

  // One session, one load. Records the status of the page's first response and of the last main-frame
  // document 8 s later (an interstitial that clears itself reloads the page), which vendor's scripts,
  // headers or cookies showed up (cookie and header NAMES only; values are never read), and the state.
  static Map<String, Object> visit(Playwright pw, Setup s, String url, String proxy, String profile) {
    try {
      BrowserType type = switch (s.type()) { case "firefox" -> pw.firefox(); case "webkit" -> pw.webkit(); default -> pw.chromium(); };
      Browser browser = type.connect(launch(s.engine(), s.headless(), proxy, profile),
          new BrowserType.ConnectOptions().setHeaders(Map.of("x-api-key", KEY)));
      try {
        BrowserContext context = profile != null ? browser.contexts().get(0) : browser.newContext();
        Page tab = context.newPage();
        boolean[] vendor = {false, false}; // kasada, aws_waf
        String[] wafAction = {null};
        List<double[]> docs = new ArrayList<>();
        long t0 = System.nanoTime();
        Frame main = tab.mainFrame();
        tab.onRequest(r -> {
          if (KASADA_URL.matcher(r.url()).find() || anyPrefix(r.headers(), "x-kpsdk")) vendor[0] = true;
          if (AWS_URL.matcher(r.url()).find()) vendor[1] = true;
        });
        tab.onResponse(r -> {
          Map<String, String> h = r.headers();
          if (anyPrefix(h, "x-kpsdk")) vendor[0] = true;
          String a = h.get("x-amzn-waf-action");
          if (a != null && !a.isEmpty()) {
            vendor[1] = true;
            if ((a.equals("challenge") || a.equals("captcha")) && wafAction[0] == null) wafAction[0] = a;
          }
          if (r.request().isNavigationRequest() && r.frame() == main) docs.add(new double[] {r.status(), (System.nanoTime() - t0) / 1e9});
        });
        int status;
        try {
          Response res = tab.navigate(url, new Page.NavigateOptions().setWaitUntil(WaitUntilState.DOMCONTENTLOADED).setTimeout(45000));
          status = res != null ? res.status() : 0;
        } catch (PlaywrightException e) {
          // A reset HTTP/2 connection is a refusal, not a network failure.
          return failed(String.valueOf(e.getMessage()).contains("ERR_HTTP2_PROTOCOL_ERROR") ? "blocked" : "error");
        }
        tab.waitForTimeout(8000);
        String outcome;
        try { outcome = (String) tab.evaluate(STATE); } catch (PlaywrightException e) { outcome = "error"; }
        int finalStatus = docs.isEmpty() ? status : (int) docs.get(docs.size() - 1)[0];
        double finalAt = docs.isEmpty() ? 0 : docs.get(docs.size() - 1)[1];
        Double clearedS = null;
        // A 202/429 check that passed by itself reloads the page with a success status: cleared, even if
        // the new page hasn't rendered yet at the 8 s mark. Without that reload it never cleared.
        if ("content".equals(outcome) || "blank".equals(outcome)) {
          if (finalStatus >= 400) outcome = "blocked";
          else if (status >= 400 || status == 202) {
            if (docs.size() > 1 && finalStatus != 202) { outcome = "cleared"; clearedS = Math.round(finalAt * 10) / 10.0; } else outcome = "blocked";
          } else if ("blank".equals(outcome)) outcome = "blocked";
        }
        TreeSet<String> cookies = new TreeSet<>();
        for (com.microsoft.playwright.options.Cookie c : context.cookies()) {
          if (VENDOR_COOKIE.matcher(c.name).find()) {
            cookies.add(c.name);
            if (c.name.toUpperCase().startsWith("KP_")) vendor[0] = true; else vendor[1] = true;
          }
        }
        Map<String, Object> m = new LinkedHashMap<>();
        m.put("outcome", outcome);
        m.put("status", status);
        m.put("final_status", finalStatus);
        if (clearedS != null) m.put("cleared_s", clearedS);
        m.put("kasada", vendor[0]);
        m.put("aws_waf", vendor[1]);
        if (wafAction[0] != null) m.put("waf_action", wafAction[0]);
        m.put("cookies", new ArrayList<>(cookies));
        return m;
      } finally {
        browser.close();
      }
    } catch (Exception e) {
      return failed("error");
    }
  }

  // One sticky residential exit per site; its setups run one after another (one session at a time per
  // site), and the sites run side by side. A network error gets one more try.
  static Map<String, Object> site(String label, String url) throws Exception {
    try (Playwright pw = Playwright.create()) {
      String proxy = "cdpfleet-resi-country-us-session-c77" + RUN + label + "-lifetime-30";
      Map<String, Object> results = new LinkedHashMap<>();
      for (Setup s : SETUPS) {
        Map<String, Object> r = visit(pw, s, url, proxy, null);
        if ("error".equals(r.get("outcome"))) r = visit(pw, s, url, proxy, null);
        results.put(s.id(), r);
      }
      // A returning visitor: Camoufox with a profile, a first visit and then a second session.
      String name = "case77-" + RUN + "-" + label;
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
    String[] urls = System.getenv("LAB_SITES_KASADA_AWSWAF").split(",");
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
