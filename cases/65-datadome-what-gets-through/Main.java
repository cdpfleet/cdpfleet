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

  record Setup(String id, String name, String engine, String type, boolean headless) {}

  // Every setup gets its own sticky residential exit (letter = setup): DataDome scores the IP as well as the browser,
  // so one setup's visits must not change what the next one sees.
  static final Map<String, String> NETWORKS = Map.of(
      "own", "own proxy (rotating)",
      "tier1", "cdpfleet-resi Tier 1, sticky US",
      "tier4", "cdpfleet-resi Tier 4, sticky US");
  static String proxy(String network, String exit) {
    return switch (network) {
      case "tier1" -> "cdpfleet-resi-country-us-session-d" + RUN + exit + "-lifetime-30";
      case "tier4" -> "cdpfleet-resi-tier-4-country-us-session-d" + RUN + exit + "-lifetime-30";
      default -> System.getenv("PROXY_URL");
    };
  }
  static final Map<String, Setup> SETUPS = Map.of(
      "chromium", new Setup("a", "Chromium, headless", "chromium", "chromium", true),
      "chrome", new Setup("b", "Google Chrome, headful", "chrome", "chromium", false),
      "edge", new Setup("c", "Microsoft Edge, headful", "edge", "chromium", false),
      "camoufox", new Setup("d", "Camoufox, defaults", "camoufox", "firefox", true));
  // DataDome's own public site, which DataDome protects: its home page and its pricing page.
  static final Map<String, String> PAGES = new LinkedHashMap<>();
  static {
    PAGES.put("home", "https://datadome.co/");
    PAGES.put("pricing", "https://datadome.co/pricing/");
  }

  // What the page shows right now, in the lab's words. DataDome answers an intercepted request with a 403 page that
  // embeds an iframe from captcha-delivery.com: a device check that passes by itself, a CAPTCHA, or a block (t=bv).
  // The same expression runs in every language.
  static final String STATE = """
      (() => {
        const f = [...document.querySelectorAll('iframe')].map((i) => i.src).find((s) => /captcha-delivery\\.com/.test(s)) || '';
        const text = (document.body ? document.body.innerText : '').trim();
        return /[?&]t=bv(&|$)/.test(f) ? 'blocked' : /\\/captcha\\//.test(f) ? 'challenge' : f ? 'checking' : text.length > 200 ? 'page' : 'loading';
      })()""";

  static Playwright playwright;

  // 503 means the fleet is momentarily busy: wait Retry-After and try again. A profile's second session can land on a
  // node with an older build of the browser (409 profile_version_downgrade): ask again for another node.
  static Browser launch(Setup s, String network, String exit, Map<String, Object> extra) throws Exception {
    JsonObject body = new JsonObject();
    body.addProperty("proxy", proxy(network, exit == null ? s.id() : exit));
    body.addProperty("headless", s.headless());
    body.addProperty("record", false);
    if (extra != null) for (var e : extra.entrySet()) body.add(e.getKey(), GSON.toJsonTree(e.getValue()));
    for (int attempt = 1; ; attempt++) {
      HttpResponse<String> res = HTTP.send(HttpRequest.newBuilder(URI.create("https://starter.cdpfleet.com/" + s.engine() + "/session"))
          .header("x-api-key", KEY).header("content-type", "application/json")
          .POST(HttpRequest.BodyPublishers.ofString(body.toString())).build(), HttpResponse.BodyHandlers.ofString());
      if (res.statusCode() == 200) {
        String wsUrl = JsonParser.parseString(res.body()).getAsJsonObject().get("wsUrl").getAsString();
        BrowserType type = s.type().equals("firefox") ? playwright.firefox() : playwright.chromium();
        return type.connect(wsUrl, new BrowserType.ConnectOptions().setHeaders(Map.of("x-api-key", KEY)));
      }
      boolean retry = res.statusCode() == 429 || res.statusCode() == 503 || (res.statusCode() == 409 && res.body().contains("profile_version_downgrade"));
      if (!retry || attempt == 8) throw new RuntimeException("launch " + res.statusCode() + " " + res.body());
      Thread.sleep(Long.parseLong(res.headers().firstValue("retry-after").orElse("5")) * 1000);
    }
  }

  static Integer goTo(Page tab, String url) {
    Response r = tab.navigate(url, new Page.NavigateOptions().setWaitUntil(WaitUntilState.DOMCONTENTLOADED).setTimeout(45000));
    return r == null ? null : r.status();
  }

  static JsonObject cell(String outcome, Integer status, Integer seconds) {
    JsonObject c = new JsonObject();
    c.addProperty("outcome", outcome);
    c.addProperty("status", status);
    c.addProperty("seconds", seconds);
    return c;
  }

  // Load a page (a network failure is retried once), then watch it once a second for 12 s without touching it.
  // content = the page straight away; cleared = DataDome intercepted (403 or its device check) and let the browser
  // through by itself; challenge = a CAPTCHA (never solved); blocked = DataDome's block page.
  static JsonObject visit(Page tab, String url) throws Exception {
    long t0 = System.currentTimeMillis();
    Integer status;
    try {
      try {
        status = goTo(tab, url);
      } catch (PlaywrightException err) {
        status = goTo(tab, url);
      }
    } catch (PlaywrightException err) {
      return cell("error", null, null);
    }
    boolean checked = status != null && status == 403;
    long until = System.currentTimeMillis() + 12000; // the watch starts once the page has loaded, however slow the proxy was
    while (System.currentTimeMillis() < until) {
      String s = null;
      try {
        s = (String) tab.evaluate(STATE);
      } catch (PlaywrightException err) {
        // while the device check reloads the page
      }
      if ("blocked".equals(s) || "challenge".equals(s)) return cell(s, status, null);
      if ("checking".equals(s)) checked = true;
      if ("page".equals(s)) {
        return checked ? cell("cleared", status, (int) Math.round((System.currentTimeMillis() - t0) / 1000.0)) : cell("content", status, null);
      }
      Thread.sleep(1000);
    }
    return cell(checked ? "challenge" : "error", status, null);
  }

  // One session per setup and network; a fresh context per page so every load is a first visit.
  static JsonObject row(String setupKey, String network, List<String> pages) throws Exception {
    Setup s = SETUPS.get(setupKey);
    Browser browser = launch(s, network, null, null);
    JsonObject out = new JsonObject();
    out.addProperty("setup", s.name());
    out.addProperty("network", NETWORKS.get(network));
    try {
      for (String key : pages) {
        BrowserContext context = browser.newContext();
        out.add(key, visit(context.newPage(), PAGES.get(key)));
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

  static boolean hasCookie(BrowserContext context) {
    return context.cookies().stream().anyMatch(c -> c.name.equals("datadome"));
  }

  // A returning visitor: the same profile and the same sticky exit, two sessions a few seconds apart. The profile keeps
  // the datadome cookie the first visit was given.
  static JsonObject returning(String setupKey) throws Exception {
    Setup s = SETUPS.get(setupKey);
    String name = "case65-" + RUN + "-" + s.id();
    List<JsonObject> visits = new ArrayList<>();
    for (int i = 0; i < 2; i++) {
      Browser browser = launch(s, "tier1", s.id() + "p", Map.of("profile", name));
      try {
        BrowserContext context = browser.contexts().get(0); // the profile's own context; a newContext() is not saved
        JsonObject c = visit(context.newPage(), PAGES.get("home"));
        // DataDome's script sets its cookie a moment after the page: give it up to 5 s.
        for (int j = 0; j < 5 && !hasCookie(context); j++) Thread.sleep(1000);
        c.addProperty("cookie", hasCookie(context));
        visits.add(c);
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
    out.addProperty("setup", s.name());
    out.addProperty("network", NETWORKS.get("tier1"));
    out.add("first", visits.get(0));
    out.add("second", visits.get(1));
    return out;
  }

  // Camoufox with humanize on and off: two page views in one context, with three mouse moves between them, so DataDome's
  // script on the first page can see how the cursor moves before the second page is requested.
  static JsonObject humanize(boolean on) throws Exception {
    Browser browser = launch(SETUPS.get("camoufox"), "tier1", on ? "h1" : "h0", Map.of("humanize", on));
    try {
      Page tab = browser.newContext().newPage();
      JsonObject out = new JsonObject();
      out.addProperty("setup", "Camoufox, humanize " + (on ? "on" : "off"));
      out.addProperty("network", NETWORKS.get("tier1"));
      out.add("home", visit(tab, PAGES.get("home")));
      long t0 = System.currentTimeMillis();
      for (int[] xy : new int[][] {{200, 300}, {700, 250}, {450, 600}}) tab.mouse().move(xy[0], xy[1]);
      out.addProperty("move_seconds", Math.round((System.currentTimeMillis() - t0) / 100.0) / 10.0);
      out.add("pricing", visit(tab, PAGES.get("pricing")));
      return out;
    } finally {
      browser.close();
    }
  }

  public static void main(String[] args) throws Exception {
    JsonObject result = new JsonObject();
    try (Playwright pw = Playwright.create()) {
      playwright = pw;
      JsonArray matrix = new JsonArray();
      for (String network : List.of("own", "tier1"))
        for (String setupKey : List.of("chromium", "chrome", "edge", "camoufox")) matrix.add(row(setupKey, network, new ArrayList<>(PAGES.keySet())));
      JsonArray tier4 = new JsonArray();
      for (String setupKey : List.of("chrome", "camoufox")) tier4.add(row(setupKey, "tier4", List.of("home")));
      JsonArray profiles = new JsonArray();
      profiles.add(returning("chrome"));
      profiles.add(returning("camoufox"));
      JsonArray human = new JsonArray();
      human.add(humanize(true));
      human.add(humanize(false));
      result.addProperty("measured_at", Instant.now().toString().substring(0, 10));
      result.add("matrix", matrix);
      result.add("tier4", tier4);
      result.add("profiles", profiles);
      result.add("humanize", human);
    }
    System.out.println(GSON.toJson(result));
  }
}
