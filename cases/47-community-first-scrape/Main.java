// Maven: com.microsoft.playwright:playwright:1.60.0, com.google.code.gson:gson:2.11.0
// Run with PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1.
// env: COMMUNITY_API_KEY
import com.google.gson.*;
import com.microsoft.playwright.*;
import java.net.URI;
import java.net.http.*;
import java.util.*;
import java.util.function.Function;

public class Main {
  static final String KEY = System.getenv("COMMUNITY_API_KEY");
  static final Gson GSON = new GsonBuilder().setPrettyPrinting().disableHtmlEscaping().create();
  static final HttpClient HTTP = HttpClient.newHttpClient();

  // 503 means the fleet is momentarily busy: wait Retry-After and try again.
  static JsonObject launch(JsonObject body) throws Exception {
    for (int attempt = 1; ; attempt++) {
      HttpResponse<String> res = HTTP.send(HttpRequest.newBuilder(URI.create("https://starter.cdpfleet.com/chromium/session"))
          .header("x-api-key", KEY).header("content-type", "application/json")
          .POST(HttpRequest.BodyPublishers.ofString(body.toString())).build(), HttpResponse.BodyHandlers.ofString());
      if (res.statusCode() == 200) return JsonParser.parseString(res.body()).getAsJsonObject();
      if (res.statusCode() != 503 || attempt == 5) throw new RuntimeException("launch " + res.statusCode() + " " + res.body());
      Thread.sleep(Long.parseLong(res.headers().firstValue("retry-after").orElse("5")) * 1000);
    }
  }

  // The exit the site sees. Shared proxies drop the odd connection: retry a few times.
  static JsonObject exitOf(Page page) {
    for (int i = 1; ; i++) {
      try {
        page.navigate("http://ip-api.com/json", new Page.NavigateOptions().setTimeout(30000));
        JsonObject j = JsonParser.parseString(page.textContent("body")).getAsJsonObject();
        JsonObject out = new JsonObject();
        out.addProperty("ip", j.get("query").getAsString());
        out.addProperty("country", j.get("countryCode").getAsString());
        return out;
      } catch (RuntimeException err) {
        if (i == 4) throw err;
        page.waitForTimeout(2000 * i);
      }
    }
  }

  static JsonObject session(Playwright pw, JsonObject extra, Function<Page, JsonObject> work) throws Exception {
    JsonObject body = new JsonObject();
    body.addProperty("headless", true);
    body.addProperty("record", false);
    for (Map.Entry<String, JsonElement> e : extra.entrySet()) body.add(e.getKey(), e.getValue());
    JsonObject s = launch(body);
    Browser browser = pw.chromium().connect(s.get("wsUrl").getAsString(), new BrowserType.ConnectOptions().setHeaders(Map.of("x-api-key", KEY)));
    try {
      JsonObject out = work.apply(browser.newPage());
      out.add("weight", s.get("weight"));
      return out;
    } finally {
      browser.close(); // ends the session and stops the clock
    }
  }

  public static void main(String[] args) throws Exception {
    // 1. What the free plan includes.
    HttpResponse<String> meRes = HTTP.send(HttpRequest.newBuilder(URI.create("https://cdpfleet.com/v1/me")).header("x-api-key", KEY).build(), HttpResponse.BodyHandlers.ofString());
    JsonObject sub = JsonParser.parseString(meRes.body()).getAsJsonObject().getAsJsonObject("subscription");
    JsonObject features = sub.getAsJsonObject("features");
    JsonObject plan = new JsonObject();
    plan.add("plan", sub.get("plan_id"));
    plan.add("threads", sub.get("threads"));
    plan.add("minutes_per_thread_per_day", sub.get("daily_minutes_per_thread"));
    plan.addProperty("max_session_minutes", sub.get("max_session_seconds").getAsInt() / 60);
    plan.add("max_pages", features.get("max_pages"));
    plan.add("memory_mb", features.get("memory_mb"));
    plan.add("proxy_options", sub.has("plan_proxy") ? sub.getAsJsonObject("plan_proxy").get("options") : JsonNull.INSTANCE);

    try (Playwright pw = Playwright.create()) {
      // 2. No proxy in the launch: the plan's included proxy is used. Scrape a catalogue page.
      JsonObject first = session(pw, new JsonObject(), page -> {
        JsonObject out = new JsonObject();
        out.add("exit", exitOf(page));
        page.navigate("https://books.toscrape.com/", new Page.NavigateOptions().setTimeout(45000));
        Object books = page.evalOnSelectorAll("article.product_pod", "els => els.map(e => ({ title: e.querySelector('h3 a').getAttribute('title'), price: e.querySelector('.price_color').textContent.trim() }))");
        JsonArray list = GSON.toJsonTree(books).getAsJsonArray();
        out.addProperty("books", list.size());
        out.add("first_book", list.get(0));
        return out;
      });

      // 3. A cdpfleet-plan token picks a country and keeps the same exit across sessions.
      String token = "cdpfleet-plan-country-de-session-c47" + UUID.randomUUID().toString().substring(0, 5) + "-lifetime-10";
      JsonObject tokenBody = new JsonObject();
      tokenBody.addProperty("proxy", token);
      JsonObject a = session(pw, tokenBody, page -> { JsonObject o = new JsonObject(); o.add("exit", exitOf(page)); return o; });
      JsonObject b = session(pw, tokenBody, page -> { JsonObject o = new JsonObject(); o.add("exit", exitOf(page)); return o; });

      JsonObject defaultProxy = new JsonObject();
      defaultProxy.add("weight", first.get("weight"));
      defaultProxy.add("exit_country", first.getAsJsonObject("exit").get("country"));
      defaultProxy.add("exit_ip", first.getAsJsonObject("exit").get("ip"));
      defaultProxy.add("books", first.get("books"));
      defaultProxy.add("first_book", first.get("first_book"));
      JsonObject planToken = new JsonObject();
      planToken.addProperty("token", token.replaceAll("session-\\w+", "session-…"));
      planToken.add("first_country", a.getAsJsonObject("exit").get("country"));
      planToken.add("second_country", b.getAsJsonObject("exit").get("country"));
      planToken.addProperty("same_ip", a.getAsJsonObject("exit").get("ip").equals(b.getAsJsonObject("exit").get("ip")));
      JsonObject result = new JsonObject();
      result.add("plan", plan);
      result.add("default_proxy", defaultProxy);
      result.add("plan_token", planToken);
      System.out.println(GSON.toJson(result));
    }
  }
}
