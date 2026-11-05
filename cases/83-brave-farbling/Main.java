// Maven: com.microsoft.playwright:playwright:1.60.0, com.google.code.gson:gson:2.11.0
// Run with PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1.
// env: CDPFLEET_API_KEY, PROXY_URL (an http(s) or socks5 proxy; one load per public test page per setup)
import com.google.gson.*;
import com.microsoft.playwright.*;
import java.net.URI;
import java.net.http.*;
import java.util.*;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");
  static final Gson GSON = new GsonBuilder().setPrettyPrinting().disableHtmlEscaping().serializeNulls().create();
  static final HttpClient HTTP = HttpClient.newHttpClient();

  record Setup(String name, String engine, boolean headless) {}

  static final List<Setup> SETUPS = List.of(
      new Setup("Brave, headless", "brave", true),
      new Setup("Brave, headful", "brave", false),
      new Setup("Google Chrome, headful", "chrome", false));

  // Two made-up sites on different registrable domains, served by page routing: the browser
  // treats them as two first-party sites, and no request leaves the browser.
  static final Map<String, String> SITES = Map.of("A", "https://site-a.example/", "B", "https://site-b.test/");
  static final String HTML = "<!doctype html><title>lab 83</title><p>A page with nothing on it but this probe.</p>";

  // What a fingerprinting script reads, the same in every language: a canvas hash (drawn twice),
  // an OfflineAudioContext hash, cores, memory, the plugin list, the screen and the window size.
  static final String PROBE = """
      (async () => {
        const fnv = (s) => { let h = 0x811c9dc5; for (let i = 0; i < s.length; i++) { h ^= s.charCodeAt(i); h = Math.imul(h, 0x01000193) >>> 0; } return h.toString(16).padStart(8, '0'); };
        const draw = () => {
          const c = document.createElement('canvas'); c.width = 240; c.height = 60;
          const x = c.getContext('2d'); x.textBaseline = 'top'; x.font = '16px Arial';
          x.fillStyle = '#f60'; x.fillRect(100, 1, 62, 20);
          x.fillStyle = '#069'; x.fillText('cdpfleet fp 83 <@>', 2, 15);
          x.fillStyle = 'rgba(102, 204, 0, 0.7)'; x.fillText('cdpfleet fp 83 <@>', 4, 17);
          return c.toDataURL();
        };
        const audio = async () => {
          try {
            const ctx = new OfflineAudioContext(1, 44100, 44100);
            const o = ctx.createOscillator(); o.type = 'triangle'; o.frequency.value = 10000;
            const k = ctx.createDynamicsCompressor();
            k.threshold.value = -50; k.knee.value = 40; k.ratio.value = 12; k.attack.value = 0; k.release.value = 0.25;
            o.connect(k); k.connect(ctx.destination); o.start(0);
            const d = new Float32Array(500); (await ctx.startRendering()).copyFromChannel(d, 0, 4500);
            let s = ''; for (let i = 0; i < 500; i++) s += d[i].toFixed(10) + ',';
            return fnv(s);
          } catch (e) { return 'error: ' + e.name; }
        };
        const c1 = draw(), c2 = draw();
        const plugins = Array.from(navigator.plugins).map((p) => p.name);
        return {
          site: location.hostname,
          canvas: fnv(c1), canvas_same_in_page: c1 === c2,
          audio: await audio(),
          cores: navigator.hardwareConcurrency, memory: navigator.deviceMemory ?? null,
          plugins: fnv(plugins.join(',')), plugin_names: plugins,
          screen: screen.width + 'x' + screen.height, window: outerWidth + 'x' + outerHeight,
        };
      })()
      """;

  record TestPage(String key, String url, String ready, String read) {}

  // Each public test page's own verdict, read from its DOM; ready is polled until the page has
  // finished its checks. The same expressions run in every language.
  static final List<TestPage> PAGES = List.of(
      // A section CreepJS caught lying gets its hash marked with the "lies" class.
      new TestPage("creepjs", "https://abrahamjuliot.github.io/creepjs/",
          "/\\d+% stealth/.test(document.body.innerText) && /privacy: \\S/.test(document.body.innerText)",
          "(() => { const t = document.body.innerText.replace(/\\s+/g, ' '); const m = /(\\d+)% like headless[\\s\\S]*?(\\d+)% headless:[\\s\\S]*?(\\d+)% stealth/.exec(t); const privacy = /privacy: (\\S+)/.exec(t); const noise = /rendering: (\\d+% \\w+ noise)/.exec(t); return { like_headless_pct: m ? +m[1] : null, headless_pct: m ? +m[2] : null, stealth_pct: m ? +m[3] : null, privacy: privacy ? privacy[1] : null, canvas_noise: noise ? noise[1] : null, lies: Array.from(document.querySelectorAll('.lies')).map((e) => (e.previousElementSibling ? e.previousElementSibling.innerText.trim() : '?')) }; })()"),
      new TestPage("browserleaks", "https://browserleaks.com/canvas",
          "/Uniqueness\\s+\\d/.test(document.body.innerText)",
          "(() => { const t = document.body.innerText; const sig = /Signature\\s+([0-9A-Fa-f]{32})/.exec(t); const u = /Uniqueness\\s+([^\\n]+)/.exec(t); return { signature: sig ? sig[1] : null, uniqueness: u ? u[1].trim() : null }; })()"),
      new TestPage("sannysoft", "https://bot.sannysoft.com/",
          "document.querySelectorAll('td.passed, td.failed').length > 10",
          "(() => { const rows = Array.from(document.querySelectorAll('tr')).filter((tr) => tr.querySelector('td.passed, td.failed, td.warn')); const failed = rows.filter((tr) => tr.querySelector('td.failed')).map((tr) => tr.cells[0].innerText.replace(/\\s+/g, ' ').trim()); return { checks: rows.length, failed: failed.length, failed_names: failed }; })()"),
      new TestPage("deviceandbrowserinfo", "https://deviceandbrowserinfo.com/are_you_a_bot",
          "document.body.innerText.indexOf('\"isBot\"') >= 0",
          "(() => { const t = document.body.innerText; const s = t.indexOf('{', t.indexOf('\"isBot\"') - 10); let depth = 0, e = s; for (; e < t.length; e++) { if (t[e] === '{') depth++; if (t[e] === '}' && --depth === 0) break; } try { const j = JSON.parse(t.slice(s, e + 1)); return { is_bot: j.isBot, signals: Object.keys(j.details || {}).filter((k) => j.details[k] === true) }; } catch (err) { return { is_bot: null, signals: [] }; } })()"),
      // Only the bot verdict and the suspect score are read (the page also names proxy networks).
      new TestPage("fingerprint", "https://demo.fingerprint.com/playground",
          "/Suspect Score \\d+/.test(document.body.innerText.replace(/\\s+/g, ' '))",
          "(() => { const t = document.body.innerText.replace(/\\s+/g, ' '); const bot = /\\bBot (.+?) VPN /.exec(t); const score = /Suspect Score (\\d+)/.exec(t); return { bot: bot ? bot[1].replace(/[^\\x20-\\x7e]/g, '').replace(/\\s+/g, ' ').trim() : null, suspect_score: score ? +score[1] : null }; })()"));

  // 503 means the fleet is momentarily busy: wait Retry-After and try again.
  static String launch(String engine, boolean headless) throws Exception {
    JsonObject body = new JsonObject();
    body.addProperty("proxy", System.getenv("PROXY_URL"));
    body.addProperty("record", false);
    body.addProperty("headless", headless);
    for (int attempt = 1; ; attempt++) {
      HttpResponse<String> res = HTTP.send(HttpRequest.newBuilder(URI.create("https://starter.cdpfleet.com/" + engine + "/session"))
          .header("x-api-key", KEY).header("content-type", "application/json")
          .POST(HttpRequest.BodyPublishers.ofString(body.toString())).build(), HttpResponse.BodyHandlers.ofString());
      if (res.statusCode() == 200) return JsonParser.parseString(res.body()).getAsJsonObject().get("wsUrl").getAsString();
      if ((res.statusCode() != 429 && res.statusCode() != 503) || attempt == 5) throw new RuntimeException("launch " + res.statusCode() + " " + res.body());
      Thread.sleep(Long.parseLong(res.headers().firstValue("retry-after").orElse("5")) * 1000);
    }
  }

  static BrowserContext siteContext(Browser browser) {
    BrowserContext context = browser.newContext();
    for (String url : SITES.values()) context.route(url + "**", route -> route.fulfill(new Route.FulfillOptions().setContentType("text/html").setBody(HTML)));
    return context;
  }

  // One visit = a new tab in the given context, the probe, the tab closed.
  static JsonObject visit(BrowserContext context, String label, int launchNo, int contextNo, String site) {
    Page page = context.newPage();
    try {
      page.navigate(SITES.get(site));
      JsonObject v = new JsonObject();
      v.addProperty("visit", label);
      v.addProperty("launch", launchNo);
      v.addProperty("context", contextNo);
      for (Map.Entry<String, JsonElement> e : GSON.toJsonTree(page.evaluate(PROBE)).getAsJsonObject().entrySet()) v.add(e.getKey(), e.getValue());
      return v;
    } finally {
      page.close();
    }
  }

  public static void main(String[] args) throws Exception {
    JsonArray setups = new JsonArray();
    try (Playwright playwright = Playwright.create()) {
      BrowserType.ConnectOptions auth = new BrowserType.ConnectOptions().setHeaders(Map.of("x-api-key", KEY));
      for (Setup s : SETUPS) {
        JsonObject row = new JsonObject();
        row.addProperty("setup", s.name());
        row.addProperty("engine", s.engine());
        row.addProperty("headless", s.headless());
        JsonArray visits = new JsonArray();
        JsonObject pages = new JsonObject();
        // Launch 1: site A, site B, site A again in a new tab, then site A in a second context.
        Browser browser = playwright.chromium().connect(launch(s.engine(), s.headless()), auth);
        try {
          BrowserContext one = siteContext(browser);
          visits.add(visit(one, "site A", 1, 1, "A"));
          visits.add(visit(one, "site B", 1, 1, "B"));
          visits.add(visit(one, "site A again", 1, 1, "A"));
          BrowserContext two = siteContext(browser);
          visits.add(visit(two, "site A, second context", 1, 2, "A"));
        } finally {
          browser.close();
        }
        // Launch 2: sites A and B again, then each public test page once, in a fresh context.
        browser = playwright.chromium().connect(launch(s.engine(), s.headless()), auth);
        try {
          BrowserContext three = siteContext(browser);
          visits.add(visit(three, "site A, next launch", 2, 1, "A"));
          visits.add(visit(three, "site B, next launch", 2, 1, "B"));
          Page page = browser.newContext().newPage();
          for (TestPage p : PAGES) {
            try {
              // A proxy tunnel drop is tried once more; a page that loaded is never reloaded.
              try {
                page.navigate(p.url(), new Page.NavigateOptions().setTimeout(60000));
              } catch (PlaywrightException first) {
                page.navigate(p.url(), new Page.NavigateOptions().setTimeout(60000));
              }
              page.waitForFunction(p.ready(), null, new Page.WaitForFunctionOptions().setTimeout(45000));
              pages.add(p.key(), GSON.toJsonTree(page.evaluate(p.read())));
            } catch (PlaywrightException err) {
              String msg = err.getMessage().split("\n")[0];
              JsonObject e = new JsonObject();
              e.addProperty("error", msg.length() > 120 ? msg.substring(0, 120) : msg);
              pages.add(p.key(), e);
            }
          }
        } finally {
          browser.close();
        }
        row.add("visits", visits);
        row.add("pages", pages);
        setups.add(row);
      }
    }
    JsonObject out = new JsonObject();
    out.add("setups", setups);
    System.out.println(GSON.toJson(out));
  }
}
