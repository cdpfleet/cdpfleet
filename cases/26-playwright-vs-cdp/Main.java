// Maven: com.microsoft.playwright:playwright:1.60.0, com.google.code.gson:gson:2.11.0
// Run with PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1. env: CDPFLEET_API_KEY, PROXY_URL
import com.google.gson.*;
import com.microsoft.playwright.*;
import java.net.URI;
import java.net.http.*;
import java.util.*;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");
  static final Gson GSON = new GsonBuilder().setPrettyPrinting().disableHtmlEscaping().serializeNulls().create();
  static final HttpClient HTTP = HttpClient.newHttpClient();

  // What a page can notice about the client driving it. A debugger that has Runtime.enable'd
  // the page serializes logged errors, which reads their `stack` getter.
  static final String PROBE = """
      (async () => {
        let stackRead = false;
        const e = new Error('probe');
        Object.defineProperty(e, 'stack', { get() { stackRead = true; return ''; } });
        console.debug(e);
        await new Promise((r) => setTimeout(r, 100));
        return { stack_read_by_debugger: stackRead, webdriver: navigator.webdriver };
      })()""";

  static JsonObject launch(boolean cdp) throws Exception {
    JsonObject body = new JsonObject();
    body.addProperty("proxy", System.getenv("PROXY_URL"));
    body.addProperty("headless", "new");
    body.addProperty("cdp", cdp);
    HttpResponse<String> res = HTTP.send(HttpRequest.newBuilder(URI.create("https://starter.cdpfleet.com/chrome/session"))
        .header("x-api-key", KEY).header("content-type", "application/json")
        .POST(HttpRequest.BodyPublishers.ofString(GSON.toJson(body))).build(), HttpResponse.BodyHandlers.ofString());
    if (res.statusCode() != 200) throw new RuntimeException("launch " + res.statusCode() + " " + res.body());
    return JsonParser.parseString(res.body()).getAsJsonObject();
  }

  static JsonObject measure(Playwright playwright, String mode) throws Exception {
    boolean pwProtocol = mode.equals("playwright protocol");
    JsonObject s = launch(!pwProtocol);
    long t = System.currentTimeMillis();
    Browser browser = pwProtocol
        ? playwright.chromium().connect(s.get("wsUrl").getAsString(), new BrowserType.ConnectOptions().setHeaders(Map.of("x-api-key", KEY)))
        : playwright.chromium().connectOverCDP(s.get("cdpUrl").getAsString(), new BrowserType.ConnectOverCDPOptions().setHeaders(Map.of("x-api-key", KEY)));
    long connectMs = System.currentTimeMillis() - t;
    try {
      Page page = browser.newPage();
      List<String> logged = Collections.synchronizedList(new ArrayList<>());
      page.onConsoleMessage(m -> logged.add(m.type()));
      for (int attempt = 1; ; attempt++) { // the proxy can drop a tunnel; retry
        try { page.navigate("https://example.com/", new Page.NavigateOptions().setTimeout(60000)); break; }
        catch (PlaywrightException e) { if (attempt == 3) throw e; }
      }
      JsonObject seen = GSON.toJsonTree(page.evaluate(PROBE)).getAsJsonObject();
      page.waitForTimeout(200); // let the console event arrive
      boolean pdf;
      try { pdf = page.pdf().length > 0; } catch (PlaywrightException e) { pdf = false; } // not over this connection
      JsonObject row = new JsonObject();
      row.addProperty("mode", mode);
      row.addProperty("endpoint", pwProtocol ? "wsUrl" : "cdpUrl");
      row.addProperty("client_version_must_match", pwProtocol);
      row.addProperty("connect_ms", connectMs);
      row.addProperty("browser_version", browser.version());
      row.addProperty("console_events", logged.contains("debug"));
      row.addProperty("pdf", pdf);
      row.add("stack_read_by_debugger", seen.get("stack_read_by_debugger"));
      row.add("webdriver", seen.get("webdriver"));
      return row;
    } finally {
      browser.close();
    }
  }

  public static void main(String[] args) throws Exception {
    try (Playwright playwright = Playwright.create()) {
      JsonArray out = new JsonArray();
      out.add(measure(playwright, "playwright protocol"));
      out.add(measure(playwright, "connectOverCDP"));
      System.out.println(GSON.toJson(out));
    }
  }
}
