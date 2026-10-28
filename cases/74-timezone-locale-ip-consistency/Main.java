// Maven: com.microsoft.playwright:playwright:1.60.0, com.google.code.gson:gson:2.11.0
// Run with PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1.
// env: CDPFLEET_API_KEY (residential exits come from cdpfleet-resi tokens)
import com.google.gson.*;
import com.microsoft.playwright.*;
import java.net.URI;
import java.net.http.*;
import java.util.*;
import java.util.concurrent.*;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");
  static final String RUN = Long.toString(System.currentTimeMillis(), 36).substring(Long.toString(System.currentTimeMillis(), 36).length() - 4);
  static final String[][] COUNTRIES = {{"US", "en-US"}, {"DE", "de-DE"}, {"JP", "ja-JP"}};
  // {label, engine, launch options (JSON), match = context locale + timezone from a first lookup of the exit}
  static final String[][] SETUPS = {
    {"Camoufox (geoip on, default)", "camoufox", "{\"geoip\": true}", "no"},
    {"Camoufox, geoip: false", "camoufox", "{\"geoip\": false}", "no"},
    {"Camoufox, locale: \"pt-BR\"", "camoufox", "{\"geoip\": true, \"locale\": \"pt-BR\"}", "no"},
    {"Chrome headful, no locale", "chrome", "{\"headless\": false}", "no"},
    {"Chrome headful, context locale + timezoneId from the exit", "chrome", "{\"headless\": false}", "match"},
  };

  // What a site can read and compare, run on the exit-lookup page itself (ip-api.com's JSON, so the
  // page knows the exit): exit country and timezone, languages, Accept-Language (echoed by httpbin),
  // Intl's timezone and locale, a formatted number, and the WebRTC public address (compared with the
  // exit, never returned).
  static final String CHECK = """
(async () => {
  const exit = JSON.parse(document.body.innerText);
  const headers = await fetch('https://httpbin.org/headers').then((r) => r.json()).catch(() => ({ headers: {} }));
  const srflx = await new Promise((done) => {
    const ips = new Set();
    try {
      const pc = new RTCPeerConnection({ iceServers: [{ urls: 'stun:stun.l.google.com:19302' }] });
      pc.createDataChannel('x');
      pc.onicecandidate = (e) => { const c = e.candidate && e.candidate.candidate; if (c && c.includes(' srflx ')) ips.add(c.split(' ')[4]); };
      pc.createOffer().then((o) => pc.setLocalDescription(o));
      setTimeout(() => { pc.close(); done([...ips]); }, 4000);
    } catch { done([]); }
  });
  const intl = Intl.DateTimeFormat().resolvedOptions();
  const acceptLanguage = (headers.headers['Accept-Language'] || '').split(',')[0];
  const out = {
    exit_country: exit.countryCode, exit_timezone: exit.timezone,
    languages: navigator.languages.join(', '), accept_language: acceptLanguage,
    timezone: intl.timeZone, intl_locale: intl.locale, number: (1234567.891).toLocaleString(),
    webrtc: srflx.length === 0 ? 'none' : srflx.every((ip) => ip === exit.query) ? 'exit IP' : 'another IP',
  };
  out.mismatches = [
    out.timezone !== exit.timezone && 'timezone ≠ IP timezone',
    (navigator.language.split('-')[1] || '').toUpperCase() !== exit.countryCode && 'language region ≠ IP country',
    acceptLanguage.toLowerCase() !== navigator.language.toLowerCase() && 'Accept-Language ≠ navigator.language',
    !intl.locale.toLowerCase().startsWith(navigator.language.slice(0, 2).toLowerCase()) && 'Intl locale ≠ navigator.language',
    out.webrtc === 'another IP' && 'WebRTC shows another IP',
  ].filter(Boolean);
  return out;
})()""";

  static JsonObject measure(Playwright playwright, String code, String locale, String[] s) {
    Gson gson = new Gson();
    String cc = code.toLowerCase();
    boolean camoufox = s[1].equals("camoufox");
    JsonObject row = new JsonObject();
    row.addProperty("setup", s[0]);
    row.addProperty("country", code);
    try {
      JsonObject body = JsonParser.parseString(s[2]).getAsJsonObject();
      body.addProperty("proxy", "cdpfleet-resi-country-" + cc + "-session-c74" + cc + RUN + "-lifetime-10");
      if (!body.has("headless")) body.addProperty("headless", camoufox);
      body.addProperty("record", false);
      if (camoufox) body.addProperty("main_world_eval", true);
      HttpResponse<String> res = HttpClient.newHttpClient().send(HttpRequest.newBuilder(URI.create("https://starter.cdpfleet.com/" + s[1] + "/session"))
          .header("x-api-key", KEY).header("content-type", "application/json")
          .POST(HttpRequest.BodyPublishers.ofString(body.toString())).build(), HttpResponse.BodyHandlers.ofString());
      if (res.statusCode() != 200) { row.addProperty("error", "launch " + res.statusCode()); return row; }
      String wsUrl = JsonParser.parseString(res.body()).getAsJsonObject().get("wsUrl").getAsString();
      Browser browser = (camoufox ? playwright.firefox() : playwright.chromium()).connect(wsUrl, new BrowserType.ConnectOptions().setHeaders(Map.of("x-api-key", KEY)));
      try {
        Browser.NewContextOptions options = new Browser.NewContextOptions();
        if (s[3].equals("match")) { // look the exit up first, then set the context to match it
          BrowserContext probe = browser.newContext();
          Page pg = probe.newPage();
          pg.navigate("http://ip-api.com/json/?fields=timezone", new Page.NavigateOptions().setTimeout(60000));
          options.setLocale(locale).setTimezoneId(JsonParser.parseString(pg.textContent("body")).getAsJsonObject().get("timezone").getAsString());
          probe.close();
        }
        Page page = browser.newContext(options).newPage();
        page.navigate("http://ip-api.com/json/?fields=countryCode,timezone,query", new Page.NavigateOptions().setTimeout(60000));
        JsonObject seen = gson.toJsonTree(page.evaluate(camoufox ? "mw:" + CHECK : CHECK)).getAsJsonObject();
        for (String k : seen.keySet()) row.add(k, seen.get(k));
      } finally {
        browser.close();
      }
    } catch (Exception err) {
      row.addProperty("error", String.valueOf(err.getMessage()).split("\n")[0]);
    }
    return row;
  }

  public static void main(String[] args) throws Exception {
    // Countries in parallel, setups one after another within a country (one exit per country).
    ExecutorService pool = Executors.newFixedThreadPool(COUNTRIES.length);
    List<Future<JsonArray>> futures = new ArrayList<>();
    for (String[] c : COUNTRIES) futures.add(pool.submit(() -> {
      JsonArray rows = new JsonArray();
      try (Playwright playwright = Playwright.create()) { // Playwright objects are per thread
        for (String[] s : SETUPS) rows.add(measure(playwright, c[0], c[1], s));
      }
      return rows;
    }));
    JsonArray all = new JsonArray();
    for (Future<JsonArray> f : futures) all.addAll(f.get());
    pool.shutdown();
    System.out.println(new GsonBuilder().setPrettyPrinting().disableHtmlEscaping().create().toJson(all));
  }
}
