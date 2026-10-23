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
import java.util.concurrent.atomic.AtomicInteger;
import java.util.function.Predicate;
import java.util.regex.Pattern;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");
  static final Gson GSON = new GsonBuilder().setPrettyPrinting().disableHtmlEscaping().serializeNulls().create();
  static final HttpClient HTTP = HttpClient.newHttpClient();
  static final String RUN = String.format("%06x", new SecureRandom().nextInt(1 << 24));

  record Network(String label, String proxy) {}
  record Setup(String name, String engine, String type, boolean headless) {}
  record TestPage(String url, Predicate<String> sensor) {}

  // Sticky residential exits: every setup in a comparison uses the same session name, so the same exit IP.
  static final Map<String, Network> NETWORKS = Map.of(
      "own", new Network("own proxy (rotating)", System.getenv("PROXY_URL")),
      "tier1", new Network("cdpfleet-resi Tier 1, sticky US", "cdpfleet-resi-country-us-session-c67" + RUN + "-lifetime-30"),
      "tier4", new Network("cdpfleet-resi Tier 4, sticky US", "cdpfleet-resi-tier-4-country-us-session-c67" + RUN + "-lifetime-30"));
  static final Map<String, Setup> SETUPS = Map.of(
      "chromium", new Setup("Chromium, headless", "chromium", "chromium", true),
      "chrome", new Setup("Google Chrome, headful", "chrome", "chromium", false),
      "edge", new Setup("Microsoft Edge, headful", "edge", "chromium", false),
      "camoufox", new Setup("Camoufox, defaults", "camoufox", "firefox", true));
  static final Pattern PX_HOST = Pattern.compile("(^|\\.)px-(cloud|cdn)\\.net$");
  // Each vendor's own home page, which that vendor protects with its own product.
  static final Map<String, TestPage> PAGES = new LinkedHashMap<>();
  static {
    PAGES.put("akamai", new TestPage("https://www.akamai.com/", h -> h.equals("www.akamai.com")));
    PAGES.put("human", new TestPage("https://www.humansecurity.com/", h -> PX_HOST.matcher(h).find()));
  }

  // What the page shows right now, in the lab's words. The same expression runs in every language.
  static final String STATE = """
      (() => {
        const t = (document.title + ' ' + (document.body ? document.body.innerText : '').slice(0, 400)).replace(/\\s+/g, ' ');
        const blocked = /access denied|you don't have permission to access|request blocked|reference #\\d/i.test(t);
        const challenge = !!document.querySelector('#px-captcha, #sec-if-cpt-container, #sec-cpt-if') || /press (&|and) hold|challenge validation/i.test(t);
        return blocked ? 'blocked' : challenge ? 'challenge' : 'page';
      })()""";

  static Playwright playwright;

  // 503 means the fleet is momentarily busy: wait Retry-After and try again.
  static Browser launch(Setup s, String network, String profile) throws Exception {
    JsonObject body = new JsonObject();
    body.addProperty("proxy", NETWORKS.get(network).proxy());
    body.addProperty("headless", s.headless());
    body.addProperty("record", false);
    if (profile != null) body.addProperty("profile", profile);
    for (int attempt = 1; ; attempt++) {
      HttpResponse<String> res = HTTP.send(HttpRequest.newBuilder(URI.create("https://starter.cdpfleet.com/" + s.engine() + "/session"))
          .header("x-api-key", KEY).header("content-type", "application/json")
          .POST(HttpRequest.BodyPublishers.ofString(body.toString())).build(), HttpResponse.BodyHandlers.ofString());
      if (res.statusCode() == 200) {
        String wsUrl = JsonParser.parseString(res.body()).getAsJsonObject().get("wsUrl").getAsString();
        BrowserType type = s.type().equals("firefox") ? playwright.firefox() : playwright.chromium();
        return type.connect(wsUrl, new BrowserType.ConnectOptions().setHeaders(Map.of("x-api-key", KEY)));
      }
      if ((res.statusCode() != 429 && res.statusCode() != 503) || attempt == 5) throw new RuntimeException("launch " + res.statusCode() + " " + res.body());
      Thread.sleep(Long.parseLong(res.headers().firstValue("retry-after").orElse("5")) * 1000);
    }
  }

  // Akamai's _abck carries a status between its first two "~": -1 until a sensor post is accepted. Only that
  // field is kept, never the cookie itself. HUMAN issues _px3 once its collector accepts the browser.
  static String abck(BrowserContext context) {
    return context.cookies().stream().filter(c -> c.name.equals("_abck")).findFirst()
        .map(c -> { String[] parts = c.value.split("~", -1); return parts.length > 1 ? parts[1] : "?"; }).orElse(null);
  }

  static boolean px3(BrowserContext context) {
    return context.cookies().stream().anyMatch(c -> c.name.equals("_px3"));
  }

  static String state(Page tab) {
    try {
      return (String) tab.evaluate(STATE);
    } catch (PlaywrightException err) {
      return null; // while the page navigates
    }
  }

  // One load, then watch the page once a second for 10 s without touching it, so the vendor's script
  // can post its sensor data; a network failure is retried once.
  static JsonObject visit(BrowserContext context, String key) throws Exception {
    TestPage page = PAGES.get(key);
    Page tab = context.newPage();
    AtomicInteger posts = new AtomicInteger();
    tab.onRequest(r -> {
      try {
        String host = URI.create(r.url()).getHost();
        if (r.method().equals("POST") && host != null && page.sensor().test(host)) posts.incrementAndGet();
      } catch (IllegalArgumentException ignored) {
      }
    });
    JsonObject cell = new JsonObject();
    Page.NavigateOptions nav = new Page.NavigateOptions().setWaitUntil(WaitUntilState.DOMCONTENTLOADED).setTimeout(45000);
    Response res;
    try {
      try {
        res = tab.navigate(page.url(), nav);
      } catch (PlaywrightException first) {
        res = tab.navigate(page.url(), nav);
      }
    } catch (PlaywrightException err) {
      tab.close();
      cell.addProperty("outcome", "error");
      return cell;
    }
    String atLoad = abck(context);
    Set<String> seen = new HashSet<>();
    for (int i = 0; i < 10; i++) {
      String s = state(tab);
      if (s != null) seen.add(s);
      if ("blocked".equals(s)) break;
      Thread.sleep(1000);
    }
    String last = state(tab);
    String outcome = seen.contains("blocked") || "blocked".equals(last) ? "blocked"
        : "challenge".equals(last) ? "challenge" : seen.contains("challenge") ? "cleared" : "content";
    cell.addProperty("outcome", outcome);
    if (res != null) cell.addProperty("status", res.status()); else cell.add("status", JsonNull.INSTANCE);
    cell.addProperty("sensor_posts", posts.get());
    if (key.equals("akamai")) {
      cell.addProperty("abck_at_load", atLoad);
      cell.addProperty("abck_after", abck(context));
    } else {
      cell.addProperty("px3", px3(context));
    }
    tab.close();
    return cell;
  }

  // One session per setup and network; a fresh context per page so every load is a first visit.
  static JsonObject row(String setupKey, String network, List<String> pages) throws Exception {
    Setup s = SETUPS.get(setupKey);
    Browser browser = launch(s, network, null);
    JsonObject out = new JsonObject();
    out.addProperty("setup", s.name());
    out.addProperty("network", NETWORKS.get(network).label());
    try {
      for (String key : pages) {
        BrowserContext context = browser.newContext();
        out.add(key, visit(context, key));
        context.close();
      }
    } finally {
      browser.close();
    }
    return out;
  }

  static JsonObject api(String method, String path) throws Exception {
    HttpResponse<String> res = HTTP.send(HttpRequest.newBuilder(URI.create("https://cdpfleet.com" + path))
        .header("x-api-key", KEY).method(method, HttpRequest.BodyPublishers.noBody()).build(), HttpResponse.BodyHandlers.ofString());
    try {
      return JsonParser.parseString(res.body()).getAsJsonObject();
    } catch (RuntimeException err) {
      return new JsonObject();
    }
  }

  static JsonObject profileInfo(String name) throws Exception {
    JsonObject body = api("GET", "/v1/me/profiles");
    if (!body.has("profiles")) return null;
    for (JsonElement p : body.getAsJsonArray("profiles")) if (p.getAsJsonObject().get("name").getAsString().equals(name)) return p.getAsJsonObject();
    return null;
  }

  // A returning visitor: the same profile and the same sticky exit, two sessions a few seconds apart.
  static JsonObject returning(String setupKey, String network, String key) throws Exception {
    String name = "case67-" + RUN;
    List<JsonObject> visits = new ArrayList<>();
    for (int i = 0; i < 2; i++) {
      Browser browser = launch(SETUPS.get(setupKey), network, name);
      try {
        visits.add(visit(browser.contexts().get(0), key)); // the profile's own context; a newContext() is not saved
      } finally {
        browser.close();
      }
      // The node saves the profile after the browser exits: wait until it is free again.
      for (int j = 0; j < 60; j++) {
        JsonObject info = profileInfo(name);
        if (info != null && !info.get("in_use").getAsBoolean()) break;
        Thread.sleep(2000);
      }
    }
    api("DELETE", "/v1/me/profiles/" + name);
    JsonObject out = new JsonObject();
    out.addProperty("setup", SETUPS.get(setupKey).name());
    out.addProperty("network", NETWORKS.get(network).label());
    out.addProperty("page", key);
    out.add("first", visits.get(0));
    out.add("second", visits.get(1));
    return out;
  }

  public static void main(String[] args) throws Exception {
    JsonObject result = new JsonObject();
    try (Playwright pw = Playwright.create()) {
      playwright = pw;
      List<String> keys = new ArrayList<>(PAGES.keySet());
      JsonArray matrix = new JsonArray();
      for (String network : List.of("own", "tier1"))
        for (String setupKey : List.of("chromium", "chrome", "edge", "camoufox")) matrix.add(row(setupKey, network, keys));
      JsonArray tier4 = new JsonArray();
      for (String setupKey : List.of("chrome", "camoufox")) tier4.add(row(setupKey, "tier4", keys));
      JsonObject profile = returning("chrome", "tier1", "akamai");
      result.addProperty("measured_at", Instant.now().toString().substring(0, 10));
      result.add("matrix", matrix);
      result.add("tier4", tier4);
      result.add("profile", profile);
    }
    System.out.println(GSON.toJson(result));
  }
}
