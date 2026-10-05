// Maven: com.microsoft.playwright:playwright:1.60.0, com.google.code.gson:gson:2.11.0
// Run with PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1. env: CDPFLEET_API_KEY, PROXY_URL
import com.google.gson.*;
import com.microsoft.playwright.*;
import com.microsoft.playwright.options.Cookie;
import java.net.URI;
import java.net.http.*;
import java.util.*;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");
  static final Gson GSON = new GsonBuilder().setPrettyPrinting().disableHtmlEscaping().serializeNulls().create();

  // Three visitors who must not see each other's state — in ONE browser session (1 thread).
  static final String[][] PERSONAS = {
    {"alice", "en-US", "America/New_York"},
    {"bruno", "pt-BR", "America/Sao_Paulo"},
    {"chie", "ja-JP", "Asia/Tokyo"},
  };

  static final String SEEN = "() => ({\n"
      + "  cookie: document.cookie,\n"
      + "  storage_owner: localStorage.getItem('owner'),\n"
      + "  language: navigator.language,\n"
      + "  timezone: Intl.DateTimeFormat().resolvedOptions().timeZone,\n"
      + "  clock: new Date('2026-10-05T12:00:00Z').toLocaleTimeString(),\n"
      + "})";

  public static void main(String[] args) throws Exception {
    String body = "{\"proxy\": " + GSON.toJson(System.getenv("PROXY_URL")) + ", \"headless\": \"new\"}";
    HttpResponse<String> res = HttpClient.newHttpClient().send(HttpRequest.newBuilder(URI.create("https://starter.cdpfleet.com/chromium/session"))
        .header("x-api-key", KEY).header("content-type", "application/json")
        .POST(HttpRequest.BodyPublishers.ofString(body)).build(), HttpResponse.BodyHandlers.ofString());
    if (res.statusCode() != 200) throw new RuntimeException("launch " + res.statusCode() + " " + res.body());
    JsonObject session = JsonParser.parseString(res.body()).getAsJsonObject();

    try (Playwright playwright = Playwright.create()) {
      Browser browser = playwright.chromium().connect(session.get("wsUrl").getAsString(),
          new BrowserType.ConnectOptions().setHeaders(Map.of("x-api-key", KEY)));
      try {
        List<Page> pages = new ArrayList<>();
        for (String[] p : PERSONAS) {
          // Each context is a separate profile: its own cookies, storage, locale and clock.
          BrowserContext context = browser.newContext(new Browser.NewContextOptions().setLocale(p[1]).setTimezoneId(p[2]));
          context.addCookies(List.of(new Cookie("session", p[0] + "-token").setDomain("example.com").setPath("/")));
          Page page = context.newPage();
          page.navigate("https://example.com/", new Page.NavigateOptions().setTimeout(60000));
          page.evaluate("(n) => localStorage.setItem('owner', n)", p[0]);
          pages.add(page);
        }
        JsonArray out = new JsonArray();
        for (int i = 0; i < PERSONAS.length; i++) {
          Page page = pages.get(i);
          // Read everything after all three exist, so any leak between them would show.
          JsonObject seen = GSON.toJsonTree(page.evaluate(SEEN)).getAsJsonObject();
          JsonObject ip = JsonParser.parseString(page.navigate("http://ip-api.com/json/?fields=query",
              new Page.NavigateOptions().setTimeout(60000)).text()).getAsJsonObject();
          JsonObject row = new JsonObject();
          row.addProperty("persona", PERSONAS[i][0]);
          row.add("threads", session.get("weight"));
          for (String k : new String[] {"cookie", "storage_owner", "language", "timezone", "clock"}) row.add(k, seen.get(k));
          row.add("exit_ip", ip.get("query"));
          out.add(row);
        }
        System.out.println(GSON.toJson(out));
      } finally {
        browser.close();
      }
    }
  }
}
