// Maven: com.microsoft.playwright:playwright:1.60.0, com.google.code.gson:gson:2.11.0
// Run with PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1.
// env: CDPFLEET_API_KEY, PROXY_URL
import com.google.gson.*;
import com.microsoft.playwright.*;
import java.net.URI;
import java.net.http.*;
import java.util.*;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");
  static final String PROXY = System.getenv("PROXY_URL");
  static final Gson GSON = new GsonBuilder().serializeNulls().setPrettyPrinting().disableHtmlEscaping().create();

  // Six ways to drive a remote browser. Chromium (Playwright's build) and Patchright share a
  // Chromium base, so they're the like-for-like pair; Chrome is also driven over CDP, with and
  // without a console listener; Camoufox is Firefox, driven over Playwright's Juggler, not CDP.
  record Setup(String label, String engine, String family, boolean cdp, boolean listen) {}
  static final Setup[] SETUPS = {
    new Setup("Chromium, Playwright connect", "chromium", "chromium", false, true),
    new Setup("Patchright, Playwright connect", "patchright", "chromium", false, true),
    new Setup("Chrome, Playwright connect", "chrome", "chromium", false, true),
    new Setup("Chrome, connectOverCDP", "chrome", "chromium", true, true),
    new Setup("Chrome, connectOverCDP, no console listener", "chrome", "chromium", true, false),
    new Setup("Camoufox, Playwright connect", "camoufox", "firefox", false, true),
  };

  // What a detection script can do from inside the page, run as the page's own script:
  //  - the classic tell: log an Error whose `stack` is a getter; a debugger client that previews
  //    logged objects used to read it;
  //  - an Error whose `message` is a getter, the same idea;
  //  - the cost of console.log: with a client listening to the console, every logged object is
  //    serialized for it. Logging a big object 40 times is timed against JSON.stringify of the
  //    same object (so the machine's speed cancels out), five times; the median ratio is kept.
  // The verdict is left in an attribute that every client can read.
  static final String PAGE = """
      <html><body><p>console probe</p><script>
        const out = { stack_getter_read: false, message_getter_read: false };
        const e1 = new Error('probe');
        Object.defineProperty(e1, 'stack', { get() { out.stack_getter_read = true; return ''; } });
        console.debug(e1);
        const e2 = new Error('probe');
        Object.defineProperty(e2, 'message', { get() { out.message_getter_read = true; return ''; } });
        console.log(e2);
        console.log('hello-from-page');
        const big = Array.from({ length: 1000 }, (_, i) => ({ i, s: 'x'.repeat(40), a: [1, 2, 3, { d: i }] }));
        const ratios = [];
        for (let k = 0; k < 5; k++) {
          let t = performance.now();
          for (let i = 0; i < 40; i++) console.log(big);
          const logMs = performance.now() - t;
          t = performance.now();
          for (let i = 0; i < 40; i++) JSON.stringify(big);
          ratios.push(logMs / (performance.now() - t));
        }
        ratios.sort((a, b) => a - b);
        out.log_cost_ratio = Math.round(ratios[2] * 100) / 100;
        setTimeout(() => document.documentElement.setAttribute('data-out', JSON.stringify(out)), 1000);
      </script></body></html>
      """;

  static JsonObject run(Playwright pw, Setup s) throws Exception {
    JsonObject body = new JsonObject();
    body.addProperty("proxy", PROXY);
    body.addProperty("headless", true);
    body.addProperty("record", false);
    if (s.cdp()) body.addProperty("cdp", true);
    HttpResponse<String> res = HttpClient.newHttpClient().send(HttpRequest.newBuilder(URI.create("https://starter.cdpfleet.com/" + s.engine() + "/session"))
        .header("x-api-key", KEY).header("content-type", "application/json")
        .POST(HttpRequest.BodyPublishers.ofString(body.toString())).build(), HttpResponse.BodyHandlers.ofString());
    JsonObject out = new JsonObject();
    out.addProperty("label", s.label());
    if (res.statusCode() != 200) {
      out.addProperty("error", "launch " + res.statusCode() + " " + res.body());
      return out;
    }
    JsonObject session = JsonParser.parseString(res.body()).getAsJsonObject();
    BrowserType type = s.family().equals("firefox") ? pw.firefox() : pw.chromium();
    Browser browser = s.cdp()
        ? type.connectOverCDP(session.get("cdpUrl").getAsString(), new BrowserType.ConnectOverCDPOptions().setHeaders(Map.of("x-api-key", KEY)))
        : type.connect(session.get("wsUrl").getAsString(), new BrowserType.ConnectOptions().setHeaders(Map.of("x-api-key", KEY)));
    try {
      BrowserContext context = s.cdp() ? browser.contexts().get(0) : browser.newContext();
      Page page = context.newPage();
      int[] events = {0};
      if (s.listen()) page.onConsoleMessage(m -> { if (m.text().contains("hello-from-page")) events[0]++; });
      page.setContent(PAGE);
      page.waitForSelector("html[data-out]", new Page.WaitForSelectorOptions().setTimeout(30000));
      JsonObject probe = JsonParser.parseString((String) page.evaluate("() => document.documentElement.getAttribute('data-out')")).getAsJsonObject();
      out.addProperty("engine", s.engine());
      out.addProperty("connect", s.cdp() ? "connectOverCDP" : "connect");
      out.addProperty("console_listener", s.listen());
      if (s.listen()) out.addProperty("console_events", events[0]); else out.add("console_events", JsonNull.INSTANCE);
      for (Map.Entry<String, JsonElement> e : probe.entrySet()) out.add(e.getKey(), e.getValue());
      out.addProperty("evaluate_works", ((Number) page.evaluate("() => 6 * 7")).intValue() == 42);
      return out;
    } finally {
      browser.close();
    }
  }

  public static void main(String[] args) throws Exception {
    JsonArray setups = new JsonArray();
    try (Playwright pw = Playwright.create()) {
      for (Setup s : SETUPS) setups.add(run(pw, s)); // one at a time: an idle session ends after 2 minutes by default
    }
    JsonObject result = new JsonObject();
    result.add("setups", setups);
    System.out.println(GSON.toJson(result));
  }
}
