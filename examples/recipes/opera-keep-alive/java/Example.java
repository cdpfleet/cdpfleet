// Maven: com.microsoft.playwright:playwright:1.60.0  and  com.google.code.gson:gson:2.11.0
// Run with PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1: browsers run on cdpfleet.
import com.google.gson.JsonParser;
import com.microsoft.playwright.*;
import java.net.URI;
import java.net.http.*;
import java.util.Map;

public class Example {
  public static void main(String[] args) throws Exception {
    String key = System.getenv("CDPFLEET_API_KEY");

    // 1. Launch the browser
    String body = """
        {
          "proxy": "http://user:pass@proxy.example.com:8080",
          "headless": false,
          "cdp": true,
          "keep_alive": true,
          "inactivity_timeout": "10m"
        }
        """;
    HttpResponse<String> res = HttpClient.newHttpClient().send(
        HttpRequest.newBuilder(URI.create("https://starter.cdpfleet.com/opera/session"))
            .header("x-api-key", key)
            .header("content-type", "application/json")
            .POST(HttpRequest.BodyPublishers.ofString(body))
            .build(),
        HttpResponse.BodyHandlers.ofString());
    if (res.statusCode() != 200) {
      throw new RuntimeException("launch failed: " + res.statusCode() + " " + res.body());
    }
    String cdpUrl = JsonParser.parseString(res.body()).getAsJsonObject().get("cdpUrl").getAsString();

    // 2. Connect and drive it
    try (Playwright playwright = Playwright.create()) {
      Browser browser = playwright.chromium().connectOverCDP(cdpUrl,
          new BrowserType.ConnectOverCDPOptions().setHeaders(Map.of("x-api-key", key)));
      BrowserContext context = browser.contexts().get(0); // the default context — it survives a reconnect
      Page page = context.pages().isEmpty() ? context.newPage() : context.pages().get(0);
      page.navigate("https://example.com");
      System.out.println(page.title());
      browser.close(); // with keep_alive this only disconnects: end the session with DELETE /v1/me/threads/{sessionId}, or it ends after inactivity_timeout
    }
  }
}
