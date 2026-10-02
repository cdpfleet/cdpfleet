// Maven: com.microsoft.playwright:playwright:1.60.0, com.google.code.gson:gson:2.11.0
// Run with PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1.
// env: CDPFLEET_API_KEY, PROXY_URL, SOCKS_PROXIES (comma-separated socks5:// URLs, 3 or more)
import com.google.gson.*;
import com.microsoft.playwright.*;
import java.net.URI;
import java.net.http.*;
import java.util.*;
import java.util.regex.*;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");
  static final Pattern IP = Pattern.compile("\\d{1,3}(\\.\\d{1,3}){3}");

  static String hostOf(String url) { return URI.create(url).getHost(); }

  // Each context has its own connection pool, so each reading is a fresh connection.
  // Proxies drop a connection now and then: one retry.
  static String exitIpVia(Browser browser, String url) {
    for (int attempt = 1; ; attempt++) {
      BrowserContext context = browser.newContext();
      try {
        String text = context.newPage().navigate(url, new Page.NavigateOptions().setTimeout(30000)).text();
        Matcher m = IP.matcher(text);
        return m.find() ? m.group() : "(no IP in " + url + ")";
      } catch (PlaywrightException err) {
        if (attempt == 2) return "(failed: " + err.getMessage().split("\n")[0] + ")";
      } finally {
        context.close();
      }
    }
  }

  public static void main(String[] args) throws Exception {
    String[] socks = System.getenv("SOCKS_PROXIES").split(",");
    Gson gson = new Gson();
    String body = """
        {
          "proxy": %s,
          "proxy_rules": [
            {"hosts": ["*.ident.me", "ident.me"], "proxy": %s},
            {"hosts": ["httpbin.org", "*.httpbin.org"], "proxy": [%s, %s]},
            {"hosts": ["api.ipify.org"], "proxy": %s}
          ],
          "headless": true
        }""".formatted(gson.toJson(System.getenv("PROXY_URL")), gson.toJson(socks[0]),
        gson.toJson(socks[1]), gson.toJson(socks[2]), gson.toJson(socks[0]));
    // Rule 2 round-robins over two proxies. Rule 3 is ignored on purpose: IP-lookup
    // services always use the default proxy.
    HttpResponse<String> res = HttpClient.newHttpClient().send(HttpRequest.newBuilder(URI.create("https://starter.cdpfleet.com/chromium/session"))
        .header("x-api-key", KEY).header("content-type", "application/json")
        .POST(HttpRequest.BodyPublishers.ofString(body)).build(), HttpResponse.BodyHandlers.ofString());
    if (res.statusCode() != 200) throw new RuntimeException("launch: " + res.statusCode() + " " + res.body());
    String wsUrl = JsonParser.parseString(res.body()).getAsJsonObject().get("wsUrl").getAsString();

    try (Playwright playwright = Playwright.create()) {
      Browser browser = playwright.chromium().connect(wsUrl, new BrowserType.ConnectOptions().setHeaders(Map.of("x-api-key", KEY)));
      try {
        JsonObject rules = new JsonObject();
        rules.addProperty("*.ident.me", hostOf(socks[0]));
        JsonArray pool = new JsonArray();
        pool.add(hostOf(socks[1]));
        pool.add(hostOf(socks[2]));
        rules.add("httpbin.org", pool);
        rules.addProperty("api.ipify.org", hostOf(socks[0]) + " (ignored: IP-lookup host)");
        rules.addProperty("(everything else)", "residential PROXY_URL");
        JsonArray readings = new JsonArray();
        for (String url : List.of("https://v4.ident.me/", "https://httpbin.org/ip", "https://httpbin.org/ip", "https://httpbin.org/ip",
            "https://api.ipify.org/", "https://www.cloudflare.com/cdn-cgi/trace")) {
          JsonObject r = new JsonObject();
          r.addProperty("url", url);
          r.addProperty("exit_ip", exitIpVia(browser, url));
          readings.add(r);
        }
        JsonObject out = new JsonObject();
        out.add("rules", rules);
        out.add("readings", readings);
        System.out.println(new GsonBuilder().setPrettyPrinting().disableHtmlEscaping().create().toJson(out));
      } finally {
        browser.close();
      }
    }
  }
}
