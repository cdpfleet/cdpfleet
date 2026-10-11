// Maven: com.microsoft.playwright:playwright:1.60.0, com.google.code.gson:gson:2.11.0
// Run with PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1. env: CDPFLEET_API_KEY, PROXY_URL_DE (a German proxy)
import com.google.gson.*;
import com.microsoft.playwright.*;
import java.net.URI;
import java.net.http.*;
import java.util.*;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");
  static final Gson GSON = new GsonBuilder().serializeNulls().create();

  // What a page can learn about where the visitor is, from JavaScript.
  static final String PROBE = """
      async () => {
        const geo = await new Promise((resolve) => navigator.geolocation.getCurrentPosition(
          (p) => resolve({ lat: Math.round(p.coords.latitude * 100) / 100, lon: Math.round(p.coords.longitude * 100) / 100 }),
          (e) => resolve({ error: e.code === 1 ? 'permission denied' : e.message }),
          { timeout: 5000 }));
        const perm = (await navigator.permissions.query({ name: 'geolocation' })).state;
        return { geolocation: geo, permission: perm, timezone: Intl.DateTimeFormat().resolvedOptions().timeZone, language: navigator.language, utc_offset_min: -new Date().getTimezoneOffset() };
      }""";

  public static void main(String[] args) throws Exception {
    String body = "{\"proxy\": " + GSON.toJson(System.getenv("PROXY_URL_DE")) + ", \"headless\": \"new\"}";
    HttpResponse<String> res = HttpClient.newHttpClient().send(HttpRequest.newBuilder(URI.create("https://starter.cdpfleet.com/chromium/session"))
        .header("x-api-key", KEY).header("content-type", "application/json")
        .POST(HttpRequest.BodyPublishers.ofString(body)).build(), HttpResponse.BodyHandlers.ofString());
    if (res.statusCode() != 200) throw new RuntimeException("launch: " + res.statusCode() + " " + res.body());
    String wsUrl = JsonParser.parseString(res.body()).getAsJsonObject().get("wsUrl").getAsString();

    try (Playwright playwright = Playwright.create()) {
      Browser browser = playwright.chromium().connect(wsUrl, new BrowserType.ConnectOptions().setHeaders(Map.of("x-api-key", KEY)));
      try {
        JsonObject out = new JsonObject();
        out.add("ip", JsonNull.INSTANCE);
        JsonArray contexts = new JsonArray();
        out.add("contexts", contexts);

        Map<String, Browser.NewContextOptions> setups = new LinkedHashMap<>();
        setups.put("defaults", new Browser.NewContextOptions());
        setups.put("permission granted, no position", new Browser.NewContextOptions().setPermissions(List.of("geolocation")));
        setups.put("Berlin: geolocation + timezone + locale", new Browser.NewContextOptions().setPermissions(List.of("geolocation"))
            .setGeolocation(52.52, 13.405).setTimezoneId("Europe/Berlin").setLocale("de-DE"));

        for (Map.Entry<String, Browser.NewContextOptions> setup : setups.entrySet()) {
          BrowserContext ctx = browser.newContext(setup.getValue());
          Page page = ctx.newPage();
          if (out.get("ip").isJsonNull()) {
            page.navigate("http://ip-api.com/json/?fields=query,countryCode,timezone", new Page.NavigateOptions().setTimeout(60000));
            JsonObject ip = JsonParser.parseString(page.textContent("body")).getAsJsonObject();
            JsonObject where = new JsonObject();
            where.add("country", ip.get("countryCode"));
            where.add("timezone", ip.get("timezone"));
            out.add("ip", where);
          }
          page.navigate("https://httpbin.org/html", new Page.NavigateOptions().setTimeout(60000));
          JsonObject entry = new JsonObject();
          entry.addProperty("setup", setup.getKey());
          for (Map.Entry<String, JsonElement> e : GSON.toJsonTree(page.evaluate("(" + PROBE + ")()")).getAsJsonObject().entrySet()) entry.add(e.getKey(), e.getValue());
          contexts.add(entry);
          ctx.close();
        }

        System.out.println(new GsonBuilder().serializeNulls().setPrettyPrinting().disableHtmlEscaping().create().toJson(out));
      } finally {
        browser.close();
      }
    }
  }
}
