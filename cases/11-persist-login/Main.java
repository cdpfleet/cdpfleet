// Maven: com.microsoft.playwright:playwright:1.60.0, com.google.code.gson:gson:2.11.0
// Run with PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1. env: CDPFLEET_API_KEY, PROXY_URL
import com.google.gson.*;
import com.microsoft.playwright.*;
import java.net.URI;
import java.net.http.*;
import java.nio.file.*;
import java.util.Map;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");
  // Keep it somewhere safe: it holds the login.
  static final Path STATE_FILE = Paths.get(System.getProperty("java.io.tmpdir"), "cdpfleet-state.json");

  static JsonObject launch(String name) throws Exception {
    String body = "{\"proxy\": " + new Gson().toJson(System.getenv("PROXY_URL")) + ", \"headless\": true}";
    HttpResponse<String> res = HttpClient.newHttpClient().send(HttpRequest.newBuilder(URI.create("https://starter.cdpfleet.com/" + name + "/session"))
        .header("x-api-key", KEY).header("content-type", "application/json")
        .POST(HttpRequest.BodyPublishers.ofString(body)).build(), HttpResponse.BodyHandlers.ofString());
    if (res.statusCode() != 200) throw new RuntimeException("launch " + name + ": " + res.statusCode() + " " + res.body());
    return JsonParser.parseString(res.body()).getAsJsonObject();
  }

  static JsonElement cookiesSeen(Page page) {
    return JsonParser.parseString(page.navigate("https://httpbin.org/cookies", new Page.NavigateOptions().setTimeout(60000)).text())
        .getAsJsonObject().get("cookies");
  }

  public static void main(String[] args) throws Exception {
    try (Playwright playwright = Playwright.create()) {
      Map<String, String> headers = Map.of("x-api-key", KEY);
      // Session 1 (Chromium): "log in", then save cookies + localStorage to a local file.
      JsonObject s1 = launch("chromium");
      Browser b1 = playwright.chromium().connect(s1.get("wsUrl").getAsString(), new BrowserType.ConnectOptions().setHeaders(headers));
      JsonObject saved;
      try {
        BrowserContext ctx = b1.newContext();
        Page page = ctx.newPage();
        page.navigate("https://httpbin.org/cookies/set?session=abc123&user=alice", new Page.NavigateOptions().setTimeout(60000));
        page.evaluate("localStorage.setItem('draft', 'half-written review')");
        saved = JsonParser.parseString(ctx.storageState(new BrowserContext.StorageStateOptions().setPath(STATE_FILE))).getAsJsonObject();
      } finally {
        b1.close(); // the browser is gone; only the state file remains
      }

      // Session 2 (Firefox, a fresh browser on whichever server the fleet picks): restore it.
      JsonObject s2 = launch("firefox");
      Browser b2 = playwright.firefox().connect(s2.get("wsUrl").getAsString(), new BrowserType.ConnectOptions().setHeaders(headers));
      try {
        Page page = b2.newContext(new Browser.NewContextOptions().setStorageStatePath(STATE_FILE)).newPage();
        JsonElement cookies = cookiesSeen(page);
        Object draft = page.evaluate("localStorage.getItem('draft')");
        JsonElement blankCookies = cookiesSeen(b2.newContext().newPage()); // the same browser without the state

        JsonArray savedCookies = new JsonArray();
        for (JsonElement c : saved.getAsJsonArray("cookies")) {
          savedCookies.add(c.getAsJsonObject().get("name").getAsString() + "@" + c.getAsJsonObject().get("domain").getAsString());
        }
        JsonArray origins = new JsonArray();
        for (JsonElement o : saved.getAsJsonArray("origins")) origins.add(o.getAsJsonObject().get("origin"));
        JsonObject out = new JsonObject();
        JsonObject one = new JsonObject();
        one.add("id", s1.get("sessionId"));
        one.addProperty("browser", "chromium");
        one.add("saved_cookies", savedCookies);
        one.add("saved_origins", origins);
        out.add("session_1", one);
        out.addProperty("state_file_bytes", Files.size(STATE_FILE));
        JsonObject two = new JsonObject();
        two.add("id", s2.get("sessionId"));
        two.addProperty("browser", "firefox");
        two.add("cookies_sent", cookies);
        two.addProperty("local_storage_draft", (String) draft);
        out.add("session_2", two);
        JsonObject blank = new JsonObject();
        blank.add("cookies_sent", blankCookies);
        out.add("session_2_without_state", blank);
        System.out.println(new GsonBuilder().setPrettyPrinting().disableHtmlEscaping().create().toJson(out));
      } finally {
        b2.close();
      }
    }
  }
}
