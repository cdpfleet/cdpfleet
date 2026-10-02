// Maven: com.microsoft.playwright:playwright:1.60.0, com.google.code.gson:gson:2.11.0
// Run with PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1.
// env: CDPFLEET_API_KEY, PROXY_URL, PROXY_URL_DE, SOCKS_PROXIES (comma-separated socks5:// URLs)
import com.google.gson.*;
import com.microsoft.playwright.*;
import java.net.URI;
import java.net.http.*;
import java.util.*;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");
  static final String ECHO = "wss://ws.postman-echo.com/raw"; // a public WebSocket echo server

  // Runs in the page: connect, then 20 echo round trips, one at a time.
  static final String PROBE = """
      async (url) => {
        const t0 = performance.now();
        const ws = new WebSocket(url);
        await new Promise((ok, fail) => { ws.onopen = ok; ws.onerror = () => fail(new Error('WebSocket failed')); });
        const connectMs = performance.now() - t0;
        const rtts = [];
        for (let i = 0; i < 20; i++) {
          const sent = performance.now();
          const echoed = new Promise((ok) => { ws.onmessage = (m) => ok(m.data); });
          ws.send(`ping ${i}`);
          if ((await echoed) !== `ping ${i}`) throw new Error('wrong echo');
          rtts.push(performance.now() - sent);
        }
        ws.close();
        rtts.sort((a, b) => a - b);
        return { connectMs, p50: rtts[10], p95: rtts[18], min: rtts[0] };
      }""";

  static JsonObject measureOnce(Playwright playwright, String label, String proxy) throws Exception {
    String body = "{\"proxy\": " + new Gson().toJson(proxy) + ", \"headless\": true}";
    HttpResponse<String> res = HttpClient.newHttpClient().send(HttpRequest.newBuilder(URI.create("https://starter.cdpfleet.com/chromium/session"))
        .header("x-api-key", KEY).header("content-type", "application/json")
        .POST(HttpRequest.BodyPublishers.ofString(body)).build(), HttpResponse.BodyHandlers.ofString());
    if (res.statusCode() != 200) throw new RuntimeException("launch " + res.statusCode());
    String wsUrl = JsonParser.parseString(res.body()).getAsJsonObject().get("wsUrl").getAsString();
    Browser browser = playwright.chromium().connect(wsUrl, new BrowserType.ConnectOptions().setHeaders(Map.of("x-api-key", KEY)));
    try {
      Page page = browser.newPage();
      // Where this proxy exits (also gives the page an https origin to open the socket from).
      JsonObject geo = JsonParser.parseString(page.navigate("http://ip-api.com/json/?fields=country,city",
          new Page.NavigateOptions().setTimeout(30000)).text()).getAsJsonObject();
      page.navigate("https://httpbin.org/html", new Page.NavigateOptions().setTimeout(30000));
      JsonObject r = new Gson().toJsonTree(page.evaluate(PROBE, ECHO)).getAsJsonObject();
      JsonObject out = new JsonObject();
      out.addProperty("proxy", label);
      out.addProperty("exit", geo.get("city").getAsString() + ", " + geo.get("country").getAsString());
      out.addProperty("connect_ms", Math.round(r.get("connectMs").getAsDouble()));
      out.addProperty("rtt_p50_ms", Math.round(r.get("p50").getAsDouble()));
      out.addProperty("rtt_p95_ms", Math.round(r.get("p95").getAsDouble()));
      out.addProperty("rtt_min_ms", Math.round(r.get("min").getAsDouble()));
      return out;
    } finally {
      browser.close();
    }
  }

  // Residential exits drop a connection now and then: one retry in a new session.
  static JsonObject measure(Playwright playwright, String label, String proxy) {
    for (int attempt = 1; ; attempt++) {
      try {
        return measureOnce(playwright, label, proxy);
      } catch (Exception err) {
        if (attempt == 2) {
          JsonObject out = new JsonObject();
          out.addProperty("proxy", label);
          out.addProperty("error", String.valueOf(err.getMessage()).split("\n")[0]);
          return out;
        }
      }
    }
  }

  public static void main(String[] args) throws Exception {
    Map<String, String> proxies = new LinkedHashMap<>();
    proxies.put("residential (any country)", System.getenv("PROXY_URL"));
    proxies.put("residential (Germany)", System.getenv("PROXY_URL_DE"));
    proxies.put("datacenter SOCKS5", System.getenv("SOCKS_PROXIES").split(",")[0]);
    JsonArray rows = new JsonArray();
    try (Playwright playwright = Playwright.create()) {
      for (Map.Entry<String, String> e : proxies.entrySet()) rows.add(measure(playwright, e.getKey(), e.getValue()));
    }
    JsonObject out = new JsonObject();
    out.addProperty("echo_server", ECHO);
    out.addProperty("round_trips", 20);
    out.add("results", rows);
    System.out.println(new GsonBuilder().setPrettyPrinting().disableHtmlEscaping().create().toJson(out));
  }
}
