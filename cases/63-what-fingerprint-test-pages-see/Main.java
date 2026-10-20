// Maven: com.microsoft.playwright:playwright:1.60.0, com.google.code.gson:gson:2.11.0
// Run with PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1.
// env: CDPFLEET_API_KEY, PROXY_URL (an http(s) or socks5 proxy; one page load per test page per setup)
import com.google.gson.*;
import com.microsoft.playwright.*;
import java.net.URI;
import java.net.http.*;
import java.util.*;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");
  static final Gson GSON = new GsonBuilder().setPrettyPrinting().disableHtmlEscaping().serializeNulls().create();
  static final HttpClient HTTP = HttpClient.newHttpClient();

  record Setup(String name, String engine, String family, String body) {}

  static final List<Setup> SETUPS = List.of(
      new Setup("Chromium, headless", "chromium", "chromium", "{\"headless\":\"new\"}"),
      new Setup("Google Chrome, headful", "chrome", "chromium", "{\"headless\":false}"),
      new Setup("Patchright, headful", "patchright", "chromium", "{\"headless\":false}"),
      new Setup("Camoufox, defaults", "camoufox", "firefox", "{\"headless\":true}"),
      new Setup("WebKit, headless", "webkit", "webkit", "{\"headless\":true}"));

  record TestPage(String key, String url, String ready, String read) {}

  // Each test page's own verdict, read from its DOM. The same expressions run in every language;
  // ready is polled until the page has finished its checks.
  static final List<TestPage> PAGES = List.of(
      new TestPage("sannysoft", "https://bot.sannysoft.com/",
          "document.querySelectorAll('td.passed, td.failed').length > 10",
          "(() => { const rows = Array.from(document.querySelectorAll('tr')).filter((tr) => tr.querySelector('td.passed, td.failed, td.warn')); const failed = rows.filter((tr) => tr.querySelector('td.failed')).map((tr) => tr.cells[0].innerText.replace(/\\s+/g, ' ').trim()); return { checks: rows.length, failed: failed.length, failed_names: failed }; })()"),
      new TestPage("creepjs", "https://abrahamjuliot.github.io/creepjs/",
          "/\\d+% stealth/.test(document.body.innerText)",
          "(() => { const t = document.body.innerText.replace(/\\s+/g, ' '); const m = /(\\d+)% like headless[\\s\\S]*?(\\d+)% headless:[\\s\\S]*?(\\d+)% stealth/.exec(t); return m ? { like_headless_pct: +m[1], headless_pct: +m[2], stealth_pct: +m[3] } : { like_headless_pct: null, headless_pct: null, stealth_pct: null }; })()"),
      new TestPage("deviceandbrowserinfo", "https://deviceandbrowserinfo.com/are_you_a_bot",
          "document.body.innerText.indexOf('\"isBot\"') >= 0",
          "(() => { const t = document.body.innerText; const s = t.indexOf('{', t.indexOf('\"isBot\"') - 10); let depth = 0, e = s; for (; e < t.length; e++) { if (t[e] === '{') depth++; if (t[e] === '}' && --depth === 0) break; } try { const j = JSON.parse(t.slice(s, e + 1)); return { is_bot: j.isBot, signals: Object.keys(j.details || {}).filter((k) => j.details[k] === true) }; } catch (err) { return { is_bot: null, signals: [] }; } })()"),
      // Only the bot verdict and the suspect score are read (the page also names proxy networks).
      new TestPage("fingerprint", "https://demo.fingerprint.com/playground",
          "/Suspect Score \\d+/.test(document.body.innerText.replace(/\\s+/g, ' '))",
          "(() => { const t = document.body.innerText.replace(/\\s+/g, ' '); const bot = /\\bBot (.+?) VPN /.exec(t); const score = /Suspect Score (\\d+)/.exec(t); return { bot: bot ? bot[1].replace(/[^\\x20-\\x7e]/g, '').replace(/\\s+/g, ' ').trim() : null, suspect_score: score ? +score[1] : null }; })()"));

  // 503 means the fleet is momentarily busy: wait Retry-After and try again.
  static String launch(String engine, String options) throws Exception {
    JsonObject body = JsonParser.parseString(options).getAsJsonObject();
    body.addProperty("proxy", System.getenv("PROXY_URL"));
    body.addProperty("record", false);
    for (int attempt = 1; ; attempt++) {
      HttpResponse<String> res = HTTP.send(HttpRequest.newBuilder(URI.create("https://starter.cdpfleet.com/" + engine + "/session"))
          .header("x-api-key", KEY).header("content-type", "application/json")
          .POST(HttpRequest.BodyPublishers.ofString(body.toString())).build(), HttpResponse.BodyHandlers.ofString());
      if (res.statusCode() == 200) return JsonParser.parseString(res.body()).getAsJsonObject().get("wsUrl").getAsString();
      if ((res.statusCode() != 429 && res.statusCode() != 503) || attempt == 5) throw new RuntimeException("launch " + res.statusCode() + " " + res.body());
      Thread.sleep(Long.parseLong(res.headers().firstValue("retry-after").orElse("5")) * 1000);
    }
  }

  public static void main(String[] args) throws Exception {
    JsonArray results = new JsonArray();
    try (Playwright playwright = Playwright.create()) {
      Map<String, BrowserType> types = Map.of("chromium", playwright.chromium(), "firefox", playwright.firefox(), "webkit", playwright.webkit());
      for (Setup s : SETUPS) {
        String wsUrl = launch(s.engine(), s.body());
        Browser browser = types.get(s.family()).connect(wsUrl, new BrowserType.ConnectOptions().setHeaders(Map.of("x-api-key", KEY)));
        JsonObject row = new JsonObject();
        row.addProperty("setup", s.name());
        row.addProperty("engine", s.engine());
        try {
          Page page = browser.newContext().newPage();
          for (TestPage p : PAGES) {
            try {
              page.navigate(p.url(), new Page.NavigateOptions().setTimeout(60000));
              page.waitForFunction(p.ready(), null, new Page.WaitForFunctionOptions().setTimeout(45000));
              row.add(p.key(), GSON.toJsonTree(page.evaluate(p.read())));
            } catch (PlaywrightException err) {
              String msg = err.getMessage().split("\n")[0];
              JsonObject e = new JsonObject();
              e.addProperty("error", msg.length() > 120 ? msg.substring(0, 120) : msg);
              row.add(p.key(), e);
            }
          }
        } finally {
          browser.close();
        }
        results.add(row);
      }
    }
    System.out.println(GSON.toJson(results));
  }
}
