// Maven: com.microsoft.playwright:playwright:1.60.0, com.google.code.gson:gson:2.11.0
// Run with PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1.
// env: CDPFLEET_API_KEY, PROXY_URL
import com.google.gson.*;
import com.microsoft.playwright.*;
import java.net.URI;
import java.net.http.*;
import java.time.Instant;
import java.util.*;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");
  static final Gson GSON = new GsonBuilder().setPrettyPrinting().disableHtmlEscaping().create();
  static final int WAIT_SECONDS = 120; // between the two snapshots — in a real watcher, your scheduler's interval

  // What to watch: a catalogue page (its price only changes when the shop changes it) and a live
  // market price that moves by the second.
  record Target(String name, String url, String read) {}
  static final Target[] TARGETS = {
    new Target("Book price", "https://books.toscrape.com/catalogue/a-light-in-the-attic_1000/index.html",
        "() => ({ price: document.querySelector('.product_main .price_color').textContent.trim(), stock: document.querySelector('.product_main .availability').textContent.trim().replace(/\\s+/g, ' ') })"),
    new Target("Bitcoin spot (USD)", "https://api.coinbase.com/v2/prices/BTC-USD/spot", "() => ({ price: JSON.parse(document.body.innerText).data.amount })"),
  };

  // 503 means the fleet is momentarily busy: wait Retry-After and try again.
  static JsonObject launch(JsonObject body) throws Exception {
    for (int attempt = 1; ; attempt++) {
      HttpResponse<String> res = HttpClient.newHttpClient().send(HttpRequest.newBuilder(URI.create("https://starter.cdpfleet.com/chromium/session"))
          .header("x-api-key", KEY).header("content-type", "application/json")
          .POST(HttpRequest.BodyPublishers.ofString(body.toString())).build(), HttpResponse.BodyHandlers.ofString());
      if (res.statusCode() == 200) return JsonParser.parseString(res.body()).getAsJsonObject();
      if (res.statusCode() != 503 || attempt == 5) throw new RuntimeException("launch " + res.statusCode() + " " + res.body());
      Thread.sleep(Long.parseLong(res.headers().firstValue("retry-after").orElse("5")) * 1000);
    }
  }

  // One snapshot = one short session: launch, read every target, close. Nothing is billed while
  // waiting, and an idle session would end after 2 minutes (the default) anyway.
  static JsonObject snapshot(Playwright pw) throws Exception {
    long t = System.currentTimeMillis();
    JsonObject body = new JsonObject();
    body.addProperty("proxy", System.getenv("PROXY_URL"));
    body.addProperty("headless", true);
    body.addProperty("record", false);
    JsonObject s = launch(body);
    Browser browser = pw.chromium().connect(s.get("wsUrl").getAsString(), new BrowserType.ConnectOptions().setHeaders(Map.of("x-api-key", KEY)));
    try {
      Page page = browser.newPage();
      JsonObject values = new JsonObject();
      for (Target target : TARGETS) {
        for (int attempt = 1; ; attempt++) { // proxies drop a tunnel now and then
          try {
            page.navigate(target.url(), new Page.NavigateOptions().setTimeout(45000));
            values.add(target.name(), GSON.toJsonTree(page.evaluate(target.read())));
            break;
          } catch (PlaywrightException err) {
            if (attempt == 3) throw err;
          }
        }
      }
      JsonObject out = new JsonObject();
      out.addProperty("at", Instant.now().toString());
      out.addProperty("seconds", Math.round((System.currentTimeMillis() - t) / 100.0) / 10.0);
      out.add("values", values);
      return out;
    } finally {
      browser.close();
    }
  }

  static double number(String v) {
    String digits = v.replaceAll("[^0-9.]", "");
    try { return digits.isEmpty() ? 0 : Double.parseDouble(digits); } catch (NumberFormatException e) { return 0; }
  }

  // Field-by-field diff of two snapshots, with the percentage for numeric prices.
  static JsonArray diff(JsonObject a, JsonObject b) {
    JsonArray out = new JsonArray();
    for (Target target : TARGETS) {
      JsonObject va = a.getAsJsonObject("values").getAsJsonObject(target.name());
      JsonObject vb = b.getAsJsonObject("values").getAsJsonObject(target.name());
      JsonArray changes = new JsonArray();
      for (String k : va.keySet()) {
        String x = va.get(k).getAsString(), y = vb.get(k).getAsString();
        if (x.equals(y)) continue;
        JsonObject c = new JsonObject();
        c.addProperty("field", k);
        c.addProperty("before", x);
        c.addProperty("after", y);
        double nx = number(x), ny = number(y);
        if (nx != 0 && ny != 0) c.addProperty("percent", Math.round((ny - nx) / nx * 1e4) / 100.0);
        changes.add(c);
      }
      JsonObject d = new JsonObject();
      d.addProperty("target", target.name());
      d.addProperty("changed", changes.size() > 0);
      d.add("changes", changes);
      d.add("now", vb);
      out.add(d);
    }
    return out;
  }

  public static void main(String[] args) throws Exception {
    try (Playwright pw = Playwright.create()) {
      JsonObject first = snapshot(pw);
      Thread.sleep(WAIT_SECONDS * 1000L);
      JsonObject second = snapshot(pw);
      JsonObject out = new JsonObject();
      out.addProperty("wait_seconds", WAIT_SECONDS);
      out.addProperty("billed_seconds", Math.round((first.get("seconds").getAsDouble() + second.get("seconds").getAsDouble()) * 10) / 10.0);
      out.add("first_at", first.get("at"));
      out.add("second_at", second.get("at"));
      out.add("diff", diff(first, second));
      System.out.println(GSON.toJson(out));
    }
  }
}
