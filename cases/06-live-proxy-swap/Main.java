// Maven: com.microsoft.playwright:playwright:1.60.0, com.google.code.gson:gson:2.11.0
// Run with PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1.
// env: CDPFLEET_API_KEY, PROXY_URL, SOCKS_PROXIES (comma-separated socks5:// URLs)
import com.google.gson.*;
import com.microsoft.playwright.*;
import java.net.URI;
import java.net.http.*;
import java.util.Map;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");
  static final HttpClient HTTP = HttpClient.newHttpClient();
  static final Gson GSON = new Gson();

  static JsonObject post(String url, JsonObject body) throws Exception {
    HttpResponse<String> res = HTTP.send(HttpRequest.newBuilder(URI.create(url))
        .header("x-api-key", KEY).header("content-type", "application/json")
        .POST(HttpRequest.BodyPublishers.ofString(body.toString())).build(), HttpResponse.BodyHandlers.ofString());
    if (res.statusCode() != 200) throw new RuntimeException(url + ": " + res.statusCode() + " " + res.body());
    return res.body().isBlank() ? new JsonObject() : JsonParser.parseString(res.body()).getAsJsonObject();
  }

  static JsonObject json(Page page, String url) {
    return JsonParser.parseString(page.navigate(url, new Page.NavigateOptions().setTimeout(60000)).text()).getAsJsonObject();
  }

  static String exitIp(Page page, String host) { return json(page, "https://" + host + "/?format=json").get("ip").getAsString(); }

  static JsonElement cookies(Page page) { return json(page, "https://httpbin.org/cookies").get("cookies"); }

  public static void main(String[] args) throws Exception {
    JsonObject options = new JsonObject();
    options.addProperty("proxy", System.getenv("PROXY_URL"));
    options.addProperty("proxy_updatable", true);
    options.addProperty("headless", true);
    JsonObject session = post("https://starter.cdpfleet.com/chromium/session", options);
    String wsUrl = session.get("wsUrl").getAsString();

    try (Playwright playwright = Playwright.create()) {
      Browser browser = playwright.chromium().connect(wsUrl, new BrowserType.ConnectOptions().setHeaders(Map.of("x-api-key", KEY)));
      try {
        BrowserContext context = browser.newContext();
        Page page = context.newPage();
        page.navigate("https://httpbin.org/cookies/set?cart=3-items&login=alice", new Page.NavigateOptions().setTimeout(60000));
        page.evaluate("localStorage.setItem('draft', 'half-written review')");
        JsonObject before = new JsonObject();
        before.addProperty("exit_ip", exitIp(page, "api.ipify.org"));
        before.add("cookies", cookies(page));

        // Swap the proxy on the session's router (the host in wsUrl). Takes effect for new
        // connections; the browser, its tabs, cookies and storage stay as they are.
        long t = System.currentTimeMillis();
        JsonObject swap = new JsonObject();
        swap.add("session_id", session.get("sessionId"));
        swap.addProperty("proxy", System.getenv("SOCKS_PROXIES").split(",")[0]);
        post("https://" + URI.create(wsUrl).getHost() + "/admin/session/proxy", swap);
        long swapMs = System.currentTimeMillis() - t;

        // 1. Same tab, same host: the open keep-alive connection still goes through the old proxy.
        String reusedConnection = exitIp(page, "api.ipify.org");
        // 2. Same tab, a host we haven't connected to yet: a new connection, so the new proxy.
        String newConnection = exitIp(page, "api64.ipify.org");
        // 3. Move everything over: a new context (its own connection pool) with the old
        //    cookies and localStorage.
        BrowserContext moved = browser.newContext(new Browser.NewContextOptions().setStorageState(context.storageState()));
        context.close();
        Page page2 = moved.newPage();
        JsonObject after = new JsonObject();
        after.addProperty("exit_ip", exitIp(page2, "api.ipify.org"));
        after.add("cookies", cookies(page2));
        after.add("local_storage", GSON.toJsonTree(page2.evaluate("localStorage.getItem('draft')")));

        JsonObject out = new JsonObject();
        out.add("before", before);
        out.addProperty("swap_ms", swapMs);
        out.addProperty("same_tab_reused_connection", reusedConnection);
        out.addProperty("same_tab_new_connection", newConnection);
        out.add("new_context_with_storage_state", after);
        out.addProperty("same_browser_session", browser.isConnected());
        System.out.println(new GsonBuilder().setPrettyPrinting().disableHtmlEscaping().create().toJson(out));
      } finally {
        browser.close();
      }
    }
  }
}
