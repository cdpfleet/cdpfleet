// Maven: com.microsoft.playwright:playwright:1.60.0, com.google.code.gson:gson:2.11.0
// Run with PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1.
// env: CDPFLEET_API_KEY, PROXY_URL (one page load per page per setup and network; nothing is clicked or solved)
import com.google.gson.*;
import com.microsoft.playwright.*;
import com.microsoft.playwright.options.WaitUntilState;
import java.net.URI;
import java.net.http.*;
import java.security.SecureRandom;
import java.time.Instant;
import java.util.*;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");
  static final Gson GSON = new GsonBuilder().setPrettyPrinting().disableHtmlEscaping().serializeNulls().create();
  static final HttpClient HTTP = HttpClient.newHttpClient();
  static final String RUN = String.format("%06x", new SecureRandom().nextInt(1 << 24));

  record Network(String label, String proxy) {}
  record Setup(String name, String engine, String type, boolean headless) {}

  // The sticky residential exit is shared by every setup, so a difference between rows is the browser.
  static final Map<String, Network> NETWORKS = Map.of(
      "own", new Network("own proxy (rotating)", System.getenv("PROXY_URL")),
      "tier1", new Network("cdpfleet-resi Tier 1, sticky US", "cdpfleet-resi-country-us-session-c68" + RUN + "-lifetime-30"));
  static final Map<String, Setup> SETUPS = new LinkedHashMap<>();
  static {
    SETUPS.put("chromium", new Setup("Chromium, headless", "chromium", "chromium", true));
    SETUPS.put("chrome", new Setup("Google Chrome, headful", "chrome", "chromium", false));
    SETUPS.put("edge", new Setup("Microsoft Edge, headful", "edge", "chromium", false));
    SETUPS.put("brave", new Setup("Brave, headful", "brave", "chromium", false));
    SETUPS.put("camoufox", new Setup("Camoufox, defaults", "camoufox", "firefox", true));
    SETUPS.put("firefox", new Setup("Firefox, headful", "firefox", "firefox", false));
    SETUPS.put("webkit", new Setup("WebKit, headful", "webkit", "webkit", false));
  }
  // Vendors' own public demo pages. Google's v3 demo asks for a score as soon as it loads and prints its
  // server's verify response (with the score) on the page; hCaptcha's demo shows a checkbox widget.
  static final String RECAPTCHA = "https://recaptcha-demo.appspot.com/recaptcha-v3-request-scores.php";
  static final String HCAPTCHA = "https://accounts.hcaptcha.com/demo";

  // Read what the page shows; the same expressions run in every language.
  static final String SCORE = "(() => { const m = /\"score\"\\s*:\\s*([0-9.]+)/.exec(document.body ? document.body.innerText : ''); return m ? Number(m[1]) : null; })()";
  static final String HC_WIDGET = "(() => { const f = document.querySelector('iframe[src*=\"hcaptcha.com\"]'); return f ? (f.getBoundingClientRect().height > 0) : false; })()";

  static Playwright playwright;

  static Browser launch(Setup s, String network) throws Exception {
    JsonObject body = new JsonObject();
    String proxy = NETWORKS.get(network).proxy();
    body.addProperty("proxy", proxy);
    body.addProperty("headless", s.headless());
    body.addProperty("record", false);
    // WebKit takes an authenticated HTTP proxy through the updatable route (see the WebKit page).
    if (s.engine().equals("webkit") && proxy.matches("^https?:.*")) body.addProperty("proxy_updatable", true);
    for (int attempt = 1; ; attempt++) {
      HttpResponse<String> res = HTTP.send(HttpRequest.newBuilder(URI.create("https://starter.cdpfleet.com/" + s.engine() + "/session"))
          .header("x-api-key", KEY).header("content-type", "application/json")
          .POST(HttpRequest.BodyPublishers.ofString(body.toString())).build(), HttpResponse.BodyHandlers.ofString());
      if (res.statusCode() == 200) {
        String wsUrl = JsonParser.parseString(res.body()).getAsJsonObject().get("wsUrl").getAsString();
        BrowserType type = s.type().equals("firefox") ? playwright.firefox() : s.type().equals("webkit") ? playwright.webkit() : playwright.chromium();
        return type.connect(wsUrl, new BrowserType.ConnectOptions().setHeaders(Map.of("x-api-key", KEY)));
      }
      if ((res.statusCode() != 429 && res.statusCode() != 503) || attempt == 5) throw new RuntimeException("launch " + res.statusCode() + " " + res.body());
      Thread.sleep(Long.parseLong(res.headers().firstValue("retry-after").orElse("5")) * 1000);
    }
  }

  static Object safe(Page tab, String expr) {
    try { return tab.evaluate(expr); } catch (PlaywrightException err) { return null; }
  }

  static boolean open(Page tab, String url) {
    try {
      tab.navigate(url, new Page.NavigateOptions().setWaitUntil(WaitUntilState.DOMCONTENTLOADED).setTimeout(45000));
      return true;
    } catch (PlaywrightException err) {
      return false;
    }
  }

  // One load; then wait up to 20 s for the score the demo prints. Nothing on the page is touched.
  static JsonObject recaptcha(BrowserContext context) throws Exception {
    Page tab = context.newPage();
    JsonObject cell = new JsonObject();
    if (!open(tab, RECAPTCHA)) {
      tab.close();
      cell.addProperty("outcome", "error");
      cell.add("score", JsonNull.INSTANCE);
      return cell;
    }
    Object score = null;
    for (int i = 0; i < 20 && score == null; i++) { Thread.sleep(1000); score = safe(tab, SCORE); }
    try { tab.close(); } catch (PlaywrightException err) { }
    cell.addProperty("outcome", score == null ? "no score" : "score");
    if (score == null) cell.add("score", JsonNull.INSTANCE); else cell.addProperty("score", ((Number) score).doubleValue());
    return cell;
  }

  // hCaptcha only decides when someone clicks its checkbox, so a load shows whether the widget renders, nothing more.
  static JsonObject hcaptcha(BrowserContext context) throws Exception {
    Page tab = context.newPage();
    JsonObject cell = new JsonObject();
    if (!open(tab, HCAPTCHA)) {
      tab.close();
      cell.addProperty("outcome", "error");
      return cell;
    }
    boolean shown = false;
    for (int i = 0; i < 12 && !shown; i++) { Thread.sleep(1000); shown = Boolean.TRUE.equals(safe(tab, HC_WIDGET)); }
    try { tab.close(); } catch (PlaywrightException err) { }
    cell.addProperty("outcome", shown ? "widget shown" : "no widget");
    return cell;
  }

  static JsonObject row(String setupKey, String network) throws Exception {
    Setup s = SETUPS.get(setupKey);
    JsonObject out = new JsonObject();
    out.addProperty("setup", s.name());
    out.addProperty("network", NETWORKS.get(network).label());
    Browser browser;
    try {
      browser = launch(s, network);
    } catch (Exception err) {
      JsonObject rc = new JsonObject();
      rc.addProperty("outcome", "error");
      rc.add("score", JsonNull.INSTANCE);
      JsonObject hc = new JsonObject();
      hc.addProperty("outcome", "error");
      out.add("recaptcha", rc);
      out.add("hcaptcha", hc);
      return out;
    }
    // A session the server ends (e.g. its memory limit) leaves the remaining cells as errors.
    try {
      BrowserContext context = browser.newContext(); // a fresh context: every load is a first visit
      out.add("recaptcha", recaptcha(context));
      context.close();
    } catch (PlaywrightException err) {
      JsonObject rc = new JsonObject();
      rc.addProperty("outcome", "error");
      rc.add("score", JsonNull.INSTANCE);
      out.add("recaptcha", rc);
    }
    try {
      BrowserContext context = browser.newContext();
      out.add("hcaptcha", hcaptcha(context));
      context.close();
    } catch (PlaywrightException err) {
      JsonObject hc = new JsonObject();
      hc.addProperty("outcome", "error");
      out.add("hcaptcha", hc);
    }
    try { browser.close(); } catch (PlaywrightException err) { }
    return out;
  }

  public static void main(String[] args) throws Exception {
    playwright = Playwright.create();
    JsonArray rows = new JsonArray();
    for (String network : List.of("own", "tier1")) for (String setupKey : SETUPS.keySet()) rows.add(row(setupKey, network));
    playwright.close();
    JsonObject result = new JsonObject();
    result.addProperty("measured_at", Instant.now().toString().substring(0, 10));
    result.add("rows", rows);
    System.out.println(GSON.toJson(result));
  }
}
