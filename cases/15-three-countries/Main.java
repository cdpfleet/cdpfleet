// Maven: com.microsoft.playwright:playwright:1.60.0, com.google.code.gson:gson:2.11.0
// Run with PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1.
// env: CDPFLEET_API_KEY, PROXY_URL_US, PROXY_URL_DE, PROXY_URL_JP (exits in each country)
import com.google.gson.*;
import com.microsoft.playwright.*;
import java.net.URI;
import java.net.http.*;
import java.util.*;
import java.util.concurrent.*;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");

  // What a localizing site reads in the page. Run in the PAGE's own JavaScript world
  // ("mw:" prefix, needs main_world_eval): Playwright's default isolated world isn't patched
  // the same way and can report the server's UTC timezone instead of the persona's.
  static final String LOCAL_VIEW = """
      (async () => {
        const position = await new Promise((ok) => navigator.geolocation.getCurrentPosition(
          (p) => ok({ lat: p.coords.latitude, lon: p.coords.longitude }), () => ok(null), { timeout: 10000 }));
        return {
          languages: navigator.languages,
          timezone: Intl.DateTimeFormat().resolvedOptions().timeZone,
          date: new Date('2026-10-01T15:30:00Z').toLocaleString(),
          number: (1234567.891).toLocaleString(),
          price: new Intl.NumberFormat(undefined, { style: 'currency', currency: 'EUR' }).format(49.9),
          position,
        };
      })()""";

  // Great-circle distance in km.
  static long km(double lat1, double lon1, double lat2, double lon2) {
    double h = Math.pow(Math.sin(Math.toRadians(lat2 - lat1) / 2), 2)
        + Math.cos(Math.toRadians(lat1)) * Math.cos(Math.toRadians(lat2)) * Math.pow(Math.sin(Math.toRadians(lon2 - lon1) / 2), 2);
    return Math.round(12742 * Math.asin(Math.sqrt(h)));
  }

  static JsonObject persona(String country, String locale, String proxy) throws Exception {
    Gson gson = new Gson();
    // geoip: Camoufox sets timezone and geolocation from the proxy's exit IP at launch.
    String body = "{\"proxy\": " + gson.toJson(proxy) + ", \"headless\": true, \"os\": \"windows\", \"locale\": " + gson.toJson(locale)
        + ", \"geoip\": true, \"main_world_eval\": true}";
    HttpResponse<String> res = HttpClient.newHttpClient().send(HttpRequest.newBuilder(URI.create("https://starter.cdpfleet.com/camoufox/session"))
        .header("x-api-key", KEY).header("content-type", "application/json")
        .POST(HttpRequest.BodyPublishers.ofString(body)).build(), HttpResponse.BodyHandlers.ofString());
    JsonObject out = new JsonObject();
    out.addProperty("country", country);
    if (res.statusCode() != 200) { out.addProperty("error", "launch " + res.statusCode() + " " + res.body()); return out; }
    String wsUrl = JsonParser.parseString(res.body()).getAsJsonObject().get("wsUrl").getAsString();
    try (Playwright playwright = Playwright.create()) { // Playwright objects are per thread
      Browser browser = playwright.firefox().connect(wsUrl, new BrowserType.ConnectOptions().setHeaders(Map.of("x-api-key", KEY)));
      try {
        BrowserContext context = browser.newContext();
        context.grantPermissions(List.of("geolocation")); // as if the visitor clicked "Allow"
        Page page = context.newPage();
        JsonObject exit = JsonParser.parseString(page.navigate("http://ip-api.com/json/?fields=country,city,timezone,lat,lon",
            new Page.NavigateOptions().setTimeout(60000)).text()).getAsJsonObject();
        page.navigate("https://httpbin.org/html", new Page.NavigateOptions().setTimeout(60000));
        JsonObject seen = gson.toJsonTree(page.evaluate("mw:" + LOCAL_VIEW)).getAsJsonObject();
        Object isolated = page.evaluate("Intl.DateTimeFormat().resolvedOptions().timeZone");
        out.addProperty("locale", locale);
        out.addProperty("exit", exit.get("city").getAsString() + ", " + exit.get("country").getAsString());
        out.add("exit_timezone", exit.get("timezone"));
        for (String k : seen.keySet()) out.add(k, seen.get(k));
        JsonElement pos = seen.get("position");
        out.add("position_km_from_exit", pos == null || pos.isJsonNull() ? JsonNull.INSTANCE : new JsonPrimitive(km(
            pos.getAsJsonObject().get("lat").getAsDouble(), pos.getAsJsonObject().get("lon").getAsDouble(),
            exit.get("lat").getAsDouble(), exit.get("lon").getAsDouble())));
        out.addProperty("isolated_world_timezone", (String) isolated); // what a default page.evaluate would have reported
        return out;
      } finally {
        browser.close();
      }
    }
  }

  public static void main(String[] args) throws Exception {
    String[][] countries = {
      {"United States", "en-US", System.getenv("PROXY_URL_US")},
      {"Germany", "de-DE", System.getenv("PROXY_URL_DE")},
      {"Japan", "ja-JP", System.getenv("PROXY_URL_JP")},
    };
    ExecutorService pool = Executors.newFixedThreadPool(countries.length);
    List<Future<JsonObject>> jobs = new ArrayList<>();
    for (String[] c : countries) jobs.add(pool.submit(() -> persona(c[0], c[1], c[2])));
    JsonArray rows = new JsonArray();
    for (Future<JsonObject> f : jobs) rows.add(f.get());
    pool.shutdown();
    System.out.println(new GsonBuilder().setPrettyPrinting().disableHtmlEscaping().create().toJson(rows));
  }
}
