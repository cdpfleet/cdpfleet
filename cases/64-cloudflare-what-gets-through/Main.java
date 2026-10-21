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
  record TestPage(String url, String kind) {}

  // Sticky residential exits: every setup in a comparison uses the same session name, so the same exit IP.
  static final Map<String, Network> NETWORKS = Map.of(
      "own", new Network("own proxy (rotating)", System.getenv("PROXY_URL")),
      "tier1", new Network("cdpfleet-resi Tier 1, sticky US", "cdpfleet-resi-country-us-session-c64" + RUN + "-lifetime-30"),
      "tier4", new Network("cdpfleet-resi Tier 4, sticky US", "cdpfleet-resi-tier-4-country-us-session-c64" + RUN + "-lifetime-30"));
  static final Map<String, Setup> SETUPS = Map.of(
      "chromium", new Setup("Chromium, headless", "chromium", "chromium", true),
      "chrome", new Setup("Google Chrome, headful", "chrome", "chromium", false),
      "edge", new Setup("Microsoft Edge, headful", "edge", "chromium", false),
      "camoufox", new Setup("Camoufox, defaults", "camoufox", "firefox", true));
  // Public test pages only: two Cloudflare challenge pages and a Turnstile test page in two widget modes.
  // "cf" pages may answer with Cloudflare's interstitial; "widget" pages embed Turnstile and get a token if it passes.
  static final Map<String, TestPage> PAGES = new LinkedHashMap<>();
  static {
    PAGES.put("nowsecure", new TestPage("https://nowsecure.nl/", "cf"));
    PAGES.put("scrapingcourse", new TestPage("https://www.scrapingcourse.com/cloudflare-challenge", "cf"));
    PAGES.put("ts_noninteractive", new TestPage("https://peet.ws/turnstile-test/non-interactive.html", "widget"));
    PAGES.put("ts_managed", new TestPage("https://peet.ws/turnstile-test/managed.html", "widget"));
  }

  // What the page shows right now, in the lab's words. The same expression runs in every language.
  static final String STATE = """
      (() => {
        const t = (document.title + ' ' + (document.body ? document.body.innerText : '').slice(0, 400)).replace(/\\s+/g, ' ');
        const el = document.querySelector('[name="cf-turnstile-response"]');
        const state = /sorry, you have been blocked|attention required|error 1020|access denied/i.test(t) ? 'blocked'
          : /just a moment|performing security verification|verify you are human|checking your browser/i.test(t) ? 'challenge' : 'page';
        return { state, token: el && el.value ? el.value.length : 0 };
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

  static boolean hasClearance(BrowserContext context) {
    return context.cookies().stream().anyMatch(c -> c.name.equals("cf_clearance"));
  }

  // One load, then watch the page once a second without touching it: an interstitial gets 20 s to
  // clear by itself, a Turnstile widget 12 s to issue a token. A network failure is retried once.
  static JsonObject visit(BrowserContext context, String key) throws Exception {
    TestPage page = PAGES.get(key);
    Page tab = context.newPage();
    long t0 = System.currentTimeMillis();
    JsonObject cell = new JsonObject();
    cell.addProperty("outcome", "challenge");
    cell.add("seconds", JsonNull.INSTANCE);
    Page.NavigateOptions nav = new Page.NavigateOptions().setWaitUntil(WaitUntilState.DOMCONTENTLOADED).setTimeout(45000);
    try {
      try {
        tab.navigate(page.url(), nav);
      } catch (PlaywrightException first) {
        tab.navigate(page.url(), nav);
      }
    } catch (PlaywrightException err) {
      tab.close();
      cell.addProperty("outcome", "error");
      return cell;
    }
    boolean sawChallenge = false;
    long deadline = t0 + (page.kind().equals("widget") ? 12000 : 20000);
    while (System.currentTimeMillis() < deadline) {
      Map<?, ?> s = null;
      try {
        s = (Map<?, ?>) tab.evaluate(STATE);
      } catch (PlaywrightException err) {
        // while the interstitial reloads the page
      }
      String state = s == null ? null : (String) s.get("state");
      long seconds = Math.round((System.currentTimeMillis() - t0) / 1000.0);
      if ("blocked".equals(state)) { cell.addProperty("outcome", "blocked"); break; }
      if ("challenge".equals(state)) sawChallenge = true;
      if (s != null && page.kind().equals("widget") && ((Number) s.get("token")).intValue() > 0) {
        cell.addProperty("outcome", "cleared");
        cell.addProperty("seconds", seconds);
        break;
      }
      if ("page".equals(state) && page.kind().equals("cf")) {
        cell.addProperty("outcome", sawChallenge ? "cleared" : "content");
        if (sawChallenge) cell.addProperty("seconds", seconds);
        break;
      }
      Thread.sleep(1000);
    }
    if (page.kind().equals("cf")) {
      // The clearance cookie can arrive a moment after the content: give it up to 6 s.
      for (int i = 0; i < 6 && !cell.get("outcome").getAsString().equals("blocked") && !hasClearance(context); i++) Thread.sleep(1000);
      cell.addProperty("cf_clearance", hasClearance(context));
    } else {
      cell.addProperty("token", cell.get("outcome").getAsString().equals("cleared"));
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
    String name = "case64-" + RUN;
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
      JsonArray matrix = new JsonArray();
      for (String network : List.of("own", "tier1"))
        for (String setupKey : List.of("chromium", "chrome", "edge", "camoufox")) matrix.add(row(setupKey, network, new ArrayList<>(PAGES.keySet())));
      JsonArray tier4 = new JsonArray();
      for (String setupKey : List.of("chrome", "camoufox")) tier4.add(row(setupKey, "tier4", List.of("nowsecure", "scrapingcourse", "ts_managed")));
      JsonObject profile = returning("camoufox", "tier1", "scrapingcourse");
      result.addProperty("measured_at", Instant.now().toString().substring(0, 10));
      result.add("matrix", matrix);
      result.add("tier4", tier4);
      result.add("profile", profile);
    }
    System.out.println(GSON.toJson(result));
  }
}
