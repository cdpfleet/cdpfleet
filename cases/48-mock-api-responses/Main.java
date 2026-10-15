// Maven: com.microsoft.playwright:playwright:1.60.0, com.google.code.gson:gson:2.11.0
// Run with PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1.
// env: CDPFLEET_API_KEY, PROXY_URL
import com.google.gson.*;
import com.microsoft.playwright.*;
import java.net.URI;
import java.net.http.*;
import java.util.*;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.regex.Pattern;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");
  static final Gson GSON = new GsonBuilder().setPrettyPrinting().disableHtmlEscaping().create();
  static final String SITE = "https://shop.example.test"; // doesn't exist: every request to it is answered by the script

  // A tiny shop front: loads products from its API, shows an error state, and a slideshow title
  // from a real public API (httpbin.org/json).
  static final String HTML = "<html><head><meta charset=\"utf-8\"></head><body><h1>Shop</h1><ul id=\"list\"></ul><p id=\"status\"></p><p id=\"title\"></p>"
      + "<img src=\"" + SITE + "/banner.png\"><img src=\"https://httpbin.org/image/png\"><script>"
      + "fetch('/api/products').then((r) => r.ok ? r.json() : Promise.reject(r.status))"
      + ".then((items) => { for (const p of items) document.getElementById('list').insertAdjacentHTML('beforeend', '<li>' + p.name + ' — ' + p.price + '</li>'); document.getElementById('status').textContent = 'ok'; })"
      + ".catch((code) => { document.getElementById('status').textContent = 'error ' + code; });"
      + "fetch('https://httpbin.org/json').then((r) => r.json()).then((j) => { document.getElementById('title').textContent = j.slideshow.title; });"
      + "</script></body></html>";

  // 503 means the fleet is momentarily busy: wait Retry-After and try again.
  static JsonObject launch(String engine, JsonObject body) throws Exception {
    for (int attempt = 1; ; attempt++) {
      HttpResponse<String> res = HttpClient.newHttpClient().send(HttpRequest.newBuilder(URI.create("https://starter.cdpfleet.com/" + engine + "/session"))
          .header("x-api-key", KEY).header("content-type", "application/json")
          .POST(HttpRequest.BodyPublishers.ofString(body.toString())).build(), HttpResponse.BodyHandlers.ofString());
      if (res.statusCode() == 200) return JsonParser.parseString(res.body()).getAsJsonObject();
      if (res.statusCode() != 503 || attempt == 5) throw new RuntimeException("launch " + engine + " " + res.statusCode() + " " + res.body());
      Thread.sleep(Long.parseLong(res.headers().firstValue("retry-after").orElse("5")) * 1000);
    }
  }

  // Open the shop with the API answering `api` ("stub" or "error"); returns what the page shows.
  static JsonObject visit(BrowserContext context, String api) {
    Page page = context.newPage();
    AtomicInteger aborted = new AtomicInteger();
    // The page itself and its API never leave the browser (the page also declares its charset).
    page.route(SITE + "/", route -> route.fulfill(new Route.FulfillOptions().setContentType("text/html; charset=utf-8").setBody(HTML)));
    page.route(SITE + "/api/products", route -> {
      if (api.equals("stub")) {
        route.fulfill(new Route.FulfillOptions().setContentType("application/json")
            .setBody("[{\"name\":\"Red mug\",\"price\":\"$9\"},{\"name\":\"Blue mug\",\"price\":\"$11\"},{\"name\":\"Teapot\",\"price\":\"$24\"}]"));
      } else {
        route.fulfill(new Route.FulfillOptions().setStatus(500).setBody("boom"));
      }
    });
    // A real response, changed on its way in.
    page.route("https://httpbin.org/json", route -> {
      APIResponse response = route.fetch();
      JsonObject json = JsonParser.parseString(response.text()).getAsJsonObject();
      JsonObject show = json.getAsJsonObject("slideshow");
      show.addProperty("title", "Patched: " + show.get("title").getAsString());
      route.fulfill(new Route.FulfillOptions().setResponse(response).setBody(json.toString()));
    });
    // Images are dropped before they're requested.
    page.route(Pattern.compile("\\.png$|/image/"), route -> { aborted.incrementAndGet(); route.abort(); });
    page.navigate(SITE + "/");
    page.waitForFunction("() => document.getElementById('status').textContent && document.getElementById('title').textContent", null, new Page.WaitForFunctionOptions().setTimeout(30000));
    JsonObject out = GSON.toJsonTree(page.evaluate("() => ({ items: [...document.querySelectorAll('#list li')].map((li) => li.textContent), status: document.getElementById('status').textContent, title: document.getElementById('title').textContent })")).getAsJsonObject();
    page.close();
    out.addProperty("images_aborted", aborted.get());
    return out;
  }

  // Does the charset in the fulfilled Content-Type alone decode non-ASCII text? (No <meta>.)
  static String charsetHeaderOnly(BrowserContext context) {
    Page page = context.newPage();
    page.route(SITE + "/plain", route -> route.fulfill(new Route.FulfillOptions().setContentType("text/html; charset=utf-8").setBody("<p>Red mug — $9</p>")));
    page.navigate(SITE + "/plain");
    String text = page.textContent("p");
    page.close();
    return text;
  }

  static JsonObject one(Playwright pw, String engine) throws Exception {
    JsonObject body = new JsonObject();
    body.addProperty("proxy", System.getenv("PROXY_URL"));
    body.addProperty("headless", true);
    body.addProperty("record", false);
    JsonObject session = launch(engine, body);
    BrowserType type = switch (engine) { case "firefox" -> pw.firefox(); case "webkit" -> pw.webkit(); default -> pw.chromium(); };
    Browser browser = type.connect(session.get("wsUrl").getAsString(), new BrowserType.ConnectOptions().setHeaders(Map.of("x-api-key", KEY)));
    try {
      BrowserContext context = browser.newContext();
      JsonObject stub = visit(context, "stub");
      JsonObject error = visit(context, "error");
      String headerOnly = charsetHeaderOnly(context);
      JsonObject out = new JsonObject();
      out.addProperty("engine", engine);
      out.add("items", stub.get("items"));
      out.add("status", stub.get("status"));
      out.add("title", stub.get("title"));
      out.add("images_aborted", stub.get("images_aborted"));
      out.add("error_status", error.get("status"));
      out.addProperty("header_only_text", headerOnly);
      return out;
    } finally {
      browser.close();
    }
  }

  public static void main(String[] args) throws Exception {
    JsonArray results = new JsonArray();
    try (Playwright pw = Playwright.create()) {
      for (String engine : new String[] {"chromium", "firefox", "webkit"}) results.add(one(pw, engine));
    }
    JsonObject out = new JsonObject();
    out.add("results", results);
    System.out.println(GSON.toJson(out));
  }
}
