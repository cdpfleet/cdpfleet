// Maven: com.microsoft.playwright:playwright:1.60.0, com.google.code.gson:gson:2.11.0
// Run with PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1.
// env: CDPFLEET_API_KEY, SOCKS_PROXIES (comma-separated sticky socks5:// URLs: one exit IP each)
import com.google.gson.*;
import com.microsoft.playwright.*;
import java.net.URI;
import java.net.http.*;
import java.util.*;
import java.util.function.Consumer;
import java.util.regex.Pattern;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");
  static final String[] PROXIES = System.getenv("SOCKS_PROXIES").split(",");
  static final Gson GSON = new GsonBuilder().setPrettyPrinting().disableHtmlEscaping().serializeNulls().create();
  static final HttpClient HTTP = HttpClient.newHttpClient();
  static final String CHALLENGE = "<title>Just a moment...</title><p>Checking your browser before accessing the site.</p>";
  static final Pattern CHALLENGE_TEXT = Pattern.compile("just a moment|captcha|access denied", Pattern.CASE_INSENSITIVE);

  static class IpState { int served; long last; long retryAt; boolean challenged; }

  // A stand-in for a rate-limited site, in front of real httpbin requests. Each request really goes
  // out through the session's proxy; the limiter decides by the IP httpbin saw (its "origin"):
  // 3 pages in a burst, then one page per 5 s (429 + Retry-After: 5), 6 pages per IP in total. An IP
  // that comes back before its Retry-After, or is over its quota, gets a challenge page from then on.
  static Consumer<Route> limiter() {
    Map<String, IpState> ips = new HashMap<>();
    return (route) -> {
      long now = System.currentTimeMillis(); // when the request arrived
      APIResponse real;
      try {
        real = route.fetch(new Route.FetchOptions().setTimeout(30000));
      } catch (PlaywrightException e) { // the proxy hiccuped
        route.fulfill(new Route.FulfillOptions().setStatus(502).setContentType("text/plain").setBody("Bad Gateway"));
        return;
      }
      if (real.status() != 200) { // httpbin itself failed
        route.fulfill(new Route.FulfillOptions().setResponse(real));
        return;
      }
      String ip = JsonParser.parseString(real.text()).getAsJsonObject().get("origin").getAsString();
      IpState s = ips.computeIfAbsent(ip, (k) -> new IpState());
      if (now < s.retryAt || s.served >= 6) s.challenged = true;
      if (s.challenged) {
        route.fulfill(new Route.FulfillOptions().setStatus(403).setContentType("text/html").setBody(CHALLENGE));
        return;
      }
      if (s.served >= 3 && now - s.last < 5000) {
        s.retryAt = now + 5000;
        route.fulfill(new Route.FulfillOptions().setStatus(429).setHeaders(Map.of("retry-after", "5")).setContentType("text/plain").setBody("Too Many Requests"));
        return;
      }
      s.served++;
      s.last = now;
      route.fulfill(new Route.FulfillOptions().setResponse(real));
    };
  }

  // What the response means: the data, a rate limit (wait), or a block (this IP is done).
  static String classify(int status, String text) {
    if (status == 429) return "rate_limited";
    if (status == 403 || status == 503 || CHALLENGE_TEXT.matcher(text).find()) return "blocked";
    return status == 200 ? "ok" : "error";
  }

  // 503 means the fleet is momentarily busy: wait Retry-After and try again.
  static JsonObject launch(JsonObject body) throws Exception {
    for (int attempt = 1; ; attempt++) {
      HttpResponse<String> res = HTTP.send(HttpRequest.newBuilder(URI.create("https://starter.cdpfleet.com/chromium/session"))
          .header("x-api-key", KEY).header("content-type", "application/json")
          .POST(HttpRequest.BodyPublishers.ofString(body.toString())).build(), HttpResponse.BodyHandlers.ofString());
      if (res.statusCode() == 200) return JsonParser.parseString(res.body()).getAsJsonObject();
      if (res.statusCode() != 503 || attempt == 5) throw new RuntimeException("launch " + res.statusCode() + " " + res.body());
      Thread.sleep(Long.parseLong(res.headers().firstValue("retry-after").orElse("5")) * 1000);
    }
  }

  // Live proxy swap on the session's router: new connections use the new proxy.
  static void swapProxy(String wsUrl, String sessionId, String proxy) throws Exception {
    JsonObject body = new JsonObject();
    body.addProperty("session_id", sessionId);
    body.addProperty("proxy", proxy);
    HttpResponse<String> res = HTTP.send(HttpRequest.newBuilder(URI.create("https://" + URI.create(wsUrl).getHost() + "/admin/session/proxy"))
        .header("x-api-key", KEY).header("content-type", "application/json")
        .POST(HttpRequest.BodyPublishers.ofString(body.toString())).build(), HttpResponse.BodyHandlers.ofString());
    if (res.statusCode() != 200) throw new RuntimeException("swap " + res.statusCode() + " " + res.body());
  }

  static JsonObject crawl(Playwright pw, boolean polite) throws Exception {
    JsonObject launchBody = new JsonObject();
    launchBody.addProperty("proxy", PROXIES[polite ? 1 : 0]);
    launchBody.addProperty("headless", "new");
    launchBody.addProperty("record", false);
    JsonObject session = launch(launchBody);
    String wsUrl = session.get("wsUrl").getAsString();
    String sessionId = session.get("sessionId").getAsString();
    Browser browser = pw.chromium().connect(wsUrl, new BrowserType.ConnectOptions().setHeaders(Map.of("x-api-key", KEY)));
    Consumer<Route> site = limiter();
    JsonArray rows = new JsonArray();
    List<String> exits = new ArrayList<>();
    int waited = 0, swaps = 0, next = 2, fetched = 0;
    boolean cookieKept;
    try {
      BrowserContext context = browser.newContext();
      context.route("https://httpbin.org/anything/**", site);
      Page page = context.newPage();
      // A logged-in state that must survive a proxy swap.
      page.navigate("https://httpbin.org/cookies/set?session=alice-42", new Page.NavigateOptions().setTimeout(60000));
      for (int i = 0; i < 8; i++) {
        List<String> trail = new ArrayList<>();
        String outcome = "gave up";
        String exitIp = null;
        for (int attempt = 1; attempt <= 6; attempt++) {
          Response res = page.navigate("https://httpbin.org/anything/catalogue/page-" + (i + 1), new Page.NavigateOptions().setTimeout(60000));
          String text = page.textContent("body");
          String kind = classify(res.status(), text);
          trail.add(String.valueOf(res.status()));
          if (kind.equals("ok")) {
            outcome = "ok";
            exitIp = JsonParser.parseString(text).getAsJsonObject().get("origin").getAsString();
            break;
          }
          if (kind.equals("rate_limited")) {
            if (!polite && attempt == 3) break;
            // Honour Retry-After; without one, back off exponentially (1, 2, 4… s, capped).
            String ra = res.headers().get("retry-after");
            int seconds = !polite ? 0 : ra != null ? Integer.parseInt(ra) : (int) Math.min(Math.pow(2, attempt - 1), 30);
            if (seconds > 0) trail.add("wait " + seconds + "s");
            waited += seconds;
            Thread.sleep(seconds * 1000L);
            continue;
          }
          if (kind.equals("blocked") && polite && next < PROXIES.length) {
            // This exit is done: move the session to another proxy and continue in a new context
            // (its own connections) that keeps the cookies and storage.
            swapProxy(wsUrl, sessionId, PROXIES[next++]);
            String state = context.storageState();
            context.close();
            context = browser.newContext(new Browser.NewContextOptions().setStorageState(state));
            context.route("https://httpbin.org/anything/**", site);
            page = context.newPage();
            swaps++;
            trail.add("swap proxy");
            continue;
          }
          if (kind.equals("error")) {
            Thread.sleep(1000); // a transient failure: try again
            continue;
          }
          break;
        }
        if (exitIp != null) {
          fetched++;
          if (!exits.contains(exitIp)) exits.add(exitIp);
        }
        JsonObject row = new JsonObject();
        row.addProperty("page", i + 1);
        row.addProperty("outcome", outcome);
        row.addProperty("exit_ip", exitIp);
        row.addProperty("trail", String.join(" → ", trail));
        rows.add(row);
      }
      page.navigate("https://httpbin.org/cookies", new Page.NavigateOptions().setTimeout(60000));
      JsonObject cookies = JsonParser.parseString(page.textContent("body")).getAsJsonObject().getAsJsonObject("cookies");
      cookieKept = cookies.has("session") && cookies.get("session").getAsString().equals("alice-42");
    } finally {
      browser.close();
    }
    JsonObject out = new JsonObject();
    out.addProperty("client", polite ? "polite" : "naive");
    out.addProperty("pages_fetched", fetched);
    JsonArray exitArray = new JsonArray();
    exits.forEach(exitArray::add);
    out.add("exits", exitArray);
    out.addProperty("waited_seconds", waited);
    out.addProperty("proxy_swaps", swaps);
    out.addProperty("session_cookie_kept", cookieKept);
    out.add("pages", rows);
    return out;
  }

  public static void main(String[] args) throws Exception {
    JsonObject out = new JsonObject();
    try (Playwright pw = Playwright.create()) {
      // Naive: retries a 429 straight away and has no plan for a block. Polite: waits as told and
      // moves to another exit when one is blocked.
      out.add("naive", crawl(pw, false));
      out.add("polite", crawl(pw, true));
    }
    System.out.println(GSON.toJson(out));
  }
}
