// Maven: com.microsoft.playwright:playwright:1.60.0, com.google.code.gson:gson:2.11.0
// Run with PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1. env: CDPFLEET_API_KEY (no proxy of your own needed)
import com.google.gson.*;
import com.microsoft.playwright.*;
import java.net.URI;
import java.net.http.*;
import java.util.*;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");
  static final Gson GSON = new GsonBuilder().setPrettyPrinting().disableHtmlEscaping().serializeNulls().create();
  static final HttpClient HTTP = HttpClient.newHttpClient();
  static final String B36 = Long.toString(System.currentTimeMillis(), 36);
  static final String SESSION = "c25" + B36.substring(B36.length() - 6); // a sticky name, 1–10 of [A-Za-z0-9_]

  // Four launches, all on the cdpfleet residential proxy — the token is the whole proxy config.
  static final String[][] RUNS = {
    {"rotating, any country", "cdpfleet-resi"},
    {"rotating, Germany", "cdpfleet-resi-country-de"},
    {"sticky US, browser 1", "cdpfleet-resi-country-us-session-" + SESSION + "-lifetime-10"},
    {"sticky US, browser 2", "cdpfleet-resi-country-us-session-" + SESSION + "-lifetime-10"},
  };

  // Residential peers drop a few percent of connections: retry a lookup up to 3 times.
  static JsonObject lookup(Page page, int i) {
    for (int attempt = 1; ; attempt++) {
      try {
        Response r = page.navigate("http://ip-api.com/json/?fields=query,countryCode&i=" + i + "-" + attempt,
            new Page.NavigateOptions().setTimeout(60000));
        return JsonParser.parseString(r.text()).getAsJsonObject();
      } catch (RuntimeException e) {
        if (attempt == 3) throw e;
      }
    }
  }

  static JsonObject run(Playwright playwright, String label, String proxy) throws Exception {
    JsonObject body = new JsonObject();
    body.addProperty("proxy", proxy);
    body.addProperty("headless", "new");
    HttpResponse<String> res = HTTP.send(HttpRequest.newBuilder(URI.create("https://starter.cdpfleet.com/chromium/session"))
        .header("x-api-key", KEY).header("content-type", "application/json")
        .POST(HttpRequest.BodyPublishers.ofString(body.toString())).build(), HttpResponse.BodyHandlers.ofString());
    JsonObject row = new JsonObject();
    row.addProperty("label", label);
    if (res.statusCode() != 200) {
      row.addProperty("error", "launch " + res.statusCode() + " " + res.body());
      return row;
    }
    String wsUrl = JsonParser.parseString(res.body()).getAsJsonObject().get("wsUrl").getAsString();
    Browser browser = playwright.chromium().connect(wsUrl, new BrowserType.ConnectOptions().setHeaders(Map.of("x-api-key", KEY)));
    try {
      Page page = browser.newPage();
      // Three lookups on separate connections: rotating exits change, sticky ones don't.
      List<JsonObject> exits = new ArrayList<>();
      for (int i = 0; i < 3; i++) exits.add(lookup(page, i));
      LinkedHashSet<String> countries = new LinkedHashSet<>();
      Set<String> ips = new HashSet<>();
      for (JsonObject e : exits) {
        countries.add(e.get("countryCode").getAsString());
        ips.add(e.get("query").getAsString());
      }
      row.addProperty("proxy", proxy.replace(SESSION, "<name>"));
      row.addProperty("countries", String.join(",", countries));
      row.addProperty("distinct_ips", ips.size());
      row.addProperty("first_ip", exits.get(0).get("query").getAsString());
      return row;
    } finally {
      browser.close();
    }
  }

  public static void main(String[] args) throws Exception {
    List<JsonObject> out = new ArrayList<>();
    try (Playwright playwright = Playwright.create()) {
      for (String[] r : RUNS) out.add(run(playwright, r[0], r[1])); // in order, so browser 2 starts after browser 1 ended
    }
    JsonObject b1 = out.get(2);
    JsonObject b2 = out.get(3);
    for (JsonObject r : out) {
      if (r.get("label").getAsString().startsWith("sticky")) {
        r.addProperty("same_ip_as_browser_1", r.get("first_ip").getAsString().equals(b1.get("first_ip").getAsString()));
      } else {
        r.add("same_ip_as_browser_1", JsonNull.INSTANCE);
      }
    }
    // The balance the traffic is billed to (metered about once a minute, so it trails a little).
    HttpResponse<String> balRes = HTTP.send(HttpRequest.newBuilder(URI.create("https://cdpfleet.com/v1/me/proxy-balance"))
        .header("x-api-key", KEY).GET().build(), HttpResponse.BodyHandlers.ofString());
    JsonObject bal = JsonParser.parseString(balRes.body()).getAsJsonObject();
    JsonArray runs = new JsonArray();
    for (JsonObject r : out) {
      r.remove("first_ip");
      runs.add(r);
    }
    JsonObject doc = new JsonObject();
    doc.add("runs", runs);
    doc.add("sticky_ip_kept_across_browsers", b2.get("same_ip_as_browser_1"));
    doc.add("balance_allowed", bal.get("allowed"));
    doc.add("price_per_gb_usd", bal.get("price_per_gb_usd"));
    System.out.println(GSON.toJson(doc));
  }
}
