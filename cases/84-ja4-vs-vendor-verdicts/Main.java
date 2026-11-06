// Maven: com.microsoft.playwright:playwright:1.60.0, com.google.code.gson:gson:2.11.0
// Run with PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1.
// env: CDPFLEET_API_KEY
import com.google.gson.*;
import com.microsoft.playwright.*;
import com.microsoft.playwright.options.WaitUntilState;
import java.net.URI;
import java.net.http.*;
import java.security.SecureRandom;
import java.time.Instant;
import java.util.*;
import java.util.function.Function;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");
  static final Gson GSON = new GsonBuilder().setPrettyPrinting().disableHtmlEscaping().serializeNulls().create();
  static final HttpClient HTTP = HttpClient.newHttpClient();
  static final String RUN = String.format("%06x", new SecureRandom().nextInt(1 << 24));

  record Setup(String id, String name, String engine, String type, Object headless) {}

  // The same twelve setups as the real-site lab cases 70-77 (Rebrowser needs a 1.52 client, so it's left out).
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

  static String launch(String engine, Object headless, String proxy) throws Exception {
    Map<String, Object> body = new LinkedHashMap<>(Map.of("proxy", proxy, "headless", headless, "record", false));
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

  // A string field of a JSON object (nested path), or null when missing or empty.
  static String str(JsonObject o, String... path) {
    JsonElement e = o;
    for (String p : path) {
      if (e == null || !e.isJsonObject()) return null;
      e = e.getAsJsonObject().get(p);
    }
    return e == null || e.isJsonNull() || e.getAsString().isEmpty() ? null : e.getAsString();
  }

  static Map<String, Object> fp(String http, String ja4, String ja4r, String peet, String h2, String h2Hash, String ua) {
    Map<String, Object> m = new LinkedHashMap<>();
    m.put("http_version", http);
    m.put("ja4", ja4);
    m.put("ja4_r", ja4r);
    m.put("peetprint_hash", peet);
    m.put("akamai_h2", h2);
    m.put("akamai_h2_hash", h2Hash);
    m.put("user_agent", ua);
    return m;
  }

  record Echo(String source, String url, Function<JsonObject, Map<String, Object>> read) {}

  // The browser itself loads a TLS echo page, which answers with the ClientHello and HTTP/2 frames
  // it received from this very browser. tls.peet.ws first; tls.browserleaks.com/json only if peet
  // doesn't answer (it has had outages). Each is one page load in a fresh tab.
  static final List<Echo> ECHOES = List.of(
      new Echo("tls.peet.ws", "https://tls.peet.ws/api/all", j -> fp(str(j, "http_version"), str(j, "tls", "ja4"), str(j, "tls", "ja4_r"),
          str(j, "tls", "peetprint_hash"), str(j, "http2", "akamai_fingerprint"), str(j, "http2", "akamai_fingerprint_hash"), str(j, "user_agent"))),
      new Echo("tls.browserleaks.com", "https://tls.browserleaks.com/json", j -> fp(null, str(j, "ja4"), str(j, "ja4_r"),
          null, str(j, "akamai_text"), str(j, "akamai_hash"), str(j, "user_agent"))));

  static Map<String, Object> fingerprint(Browser browser) {
    BrowserContext context = browser.newContext();
    String last = null;
    for (Echo echo : ECHOES) {
      Page tab = context.newPage();
      try {
        Response res = tab.navigate(echo.url(), new Page.NavigateOptions().setWaitUntil(WaitUntilState.DOMCONTENTLOADED).setTimeout(30000));
        Map<String, Object> f = echo.read().apply(JsonParser.parseString(res.text()).getAsJsonObject());
        if (f.get("ja4") != null) {
          Map<String, Object> m = new LinkedHashMap<>();
          m.put("source", echo.source());
          m.putAll(f);
          return m;
        }
        last = echo.source() + ": no ja4 in the answer";
      } catch (Exception e) {
        last = echo.source() + ": " + String.valueOf(e.getMessage()).split("\n")[0];
      } finally {
        try { tab.close(); } catch (Exception ignored) {}
      }
    }
    throw new RuntimeException(last);
  }

  public static void main(String[] args) {
    // One sticky residential exit for the whole run, and one session at a time.
    String proxy = "cdpfleet-resi-country-us-session-c84" + RUN + "-lifetime-30";
    Map<String, Object> fingerprints = new LinkedHashMap<>();
    try (Playwright pw = Playwright.create()) {
      Map<String, BrowserType> types = Map.of("chromium", pw.chromium(), "firefox", pw.firefox(), "webkit", pw.webkit());
      for (Setup s : SETUPS) {
        try {
          Browser browser = types.get(s.type()).connect(launch(s.engine(), s.headless(), proxy),
              new BrowserType.ConnectOptions().setHeaders(Map.of("x-api-key", KEY)));
          try {
            Map<String, Object> m = new LinkedHashMap<>();
            m.put("version", browser.version());
            m.putAll(fingerprint(browser));
            fingerprints.put(s.id(), m);
          } finally {
            browser.close();
          }
        } catch (Exception e) {
          String msg = String.valueOf(e.getMessage()).split("\n")[0];
          fingerprints.put(s.id(), Map.of("error", msg.substring(0, Math.min(200, msg.length()))));
        }
      }
    }
    Map<String, Object> setups = new LinkedHashMap<>();
    for (Setup s : SETUPS) setups.put(s.id(), s.name());
    Map<String, Object> out = new LinkedHashMap<>();
    out.put("measured_at", Instant.now().toString().substring(0, 10));
    out.put("setups", setups);
    out.put("fingerprints", fingerprints);
    System.out.println(GSON.toJson(out));
  }
}
