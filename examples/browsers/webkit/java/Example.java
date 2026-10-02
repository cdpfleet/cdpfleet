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
          "headless": "new"
        }
        """;
    HttpResponse<String> res = HttpClient.newHttpClient().send(
        HttpRequest.newBuilder(URI.create("https://starter.cdpfleet.com/webkit/session"))
            .header("x-api-key", key)
            .header("content-type", "application/json")
            .POST(HttpRequest.BodyPublishers.ofString(body))
            .build(),
        HttpResponse.BodyHandlers.ofString());
    if (res.statusCode() != 200) {
      throw new RuntimeException("launch failed: " + res.statusCode() + " " + res.body());
    }
    String wsUrl = JsonParser.parseString(res.body()).getAsJsonObject().get("wsUrl").getAsString();

    // 2. Connect and drive it
    try (Playwright playwright = Playwright.create()) {
      Browser browser = playwright.webkit().connect(wsUrl,
          new BrowserType.ConnectOptions().setHeaders(Map.of("x-api-key", key)));
      Page page = browser.newPage();
      page.navigate("https://example.com");
      System.out.println(page.title());
      browser.close(); // ends the session and stops billing
    }
  }
}
