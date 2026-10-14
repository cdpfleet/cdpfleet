// Maven: com.microsoft.playwright:playwright:1.60.0, com.google.code.gson:gson:2.11.0
// Run with PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1.
// env: CDPFLEET_API_KEY, PROXY_URL
import com.google.gson.*;
import com.microsoft.playwright.*;
import java.net.URI;
import java.net.http.*;
import java.util.*;
import java.util.regex.Pattern;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");
  static final Gson GSON = new GsonBuilder().setPrettyPrinting().disableHtmlEscaping().create();
  static final String SITE = "https://books.toscrape.com/";

  record Setup(String label, String engine, String family, String api) {}
  static final Setup[] SETUPS = {
    new Setup("Chrome", "chrome", "chromium", "typeof chrome !== 'undefined' && !!chrome.runtime"),
    new Setup("Firefox", "firefox", "firefox", "typeof browser !== 'undefined' && !!browser.runtime"),
  };

  // What an extension's content script does: runs in every page before the page's own scripts.
  static final String CONTENT_SCRIPT = """
      window.__myExtension = true;
      document.addEventListener('DOMContentLoaded', () => {
        const b = document.createElement('div');
        b.id = 'my-extension-banner';
        b.textContent = 'Injected on ' + location.pathname;
        document.body.prepend(b);
      });""";

  record Launched(int status, JsonObject session) {}

  // 503 means the fleet is momentarily busy: wait Retry-After and try again.
  static Launched launch(String engine, JsonObject body) throws Exception {
    for (int attempt = 1; ; attempt++) {
      HttpResponse<String> res = HttpClient.newHttpClient().send(HttpRequest.newBuilder(URI.create("https://starter.cdpfleet.com/" + engine + "/session"))
          .header("x-api-key", KEY).header("content-type", "application/json")
          .POST(HttpRequest.BodyPublishers.ofString(body.toString())).build(), HttpResponse.BodyHandlers.ofString());
      if (res.statusCode() == 200) return new Launched(res.statusCode(), JsonParser.parseString(res.body()).getAsJsonObject());
      if (res.statusCode() != 503 || attempt == 5) throw new RuntimeException("launch " + engine + " " + res.statusCode() + " " + res.body());
      Thread.sleep(Long.parseLong(res.headers().firstValue("retry-after").orElse("5")) * 1000);
    }
  }

  // Try to load an extension the way a local launch would: Chromium flags and an "extensions" list.
  // The fleet refuses browser-process fields with 400 unsupported_option, so nothing is started.
  static JsonObject tryExtensions(String engine) throws Exception {
    JsonObject body = new JsonObject();
    body.addProperty("proxy", System.getenv("PROXY_URL"));
    JsonArray args = new JsonArray();
    args.add("--load-extension=/tmp/my-extension");
    args.add("--user-agent=ARGS-WERE-APPLIED");
    body.add("args", args);
    JsonArray exts = new JsonArray();
    exts.add("/tmp/my-extension");
    body.add("extensions", exts);
    HttpResponse<String> res = HttpClient.newHttpClient().send(HttpRequest.newBuilder(URI.create("https://starter.cdpfleet.com/" + engine + "/session"))
        .header("x-api-key", KEY).header("content-type", "application/json")
        .POST(HttpRequest.BodyPublishers.ofString(body.toString())).build(), HttpResponse.BodyHandlers.ofString());
    JsonObject out = new JsonObject();
    out.addProperty("status", res.statusCode());
    try {
      JsonElement e = JsonParser.parseString(res.body()).getAsJsonObject().get("error");
      out.add("error", e == null ? JsonNull.INSTANCE : e);
    } catch (Exception e) {
      out.add("error", JsonNull.INSTANCE);
    }
    return out;
  }

  static JsonObject one(Playwright pw, Setup s) throws Exception {
    JsonObject refused = tryExtensions(s.engine());
    JsonObject out = new JsonObject();
    out.addProperty("label", s.label());
    out.add("launch_status", refused.get("status"));
    out.add("launch_error", refused.get("error"));
    // A normal launch, for the Playwright equivalents.
    JsonObject body = new JsonObject();
    body.addProperty("proxy", System.getenv("PROXY_URL"));
    Launched l = launch(s.engine(), body);
    BrowserType type = s.family().equals("firefox") ? pw.firefox() : pw.chromium();
    Browser browser = type.connect(l.session().get("wsUrl").getAsString(), new BrowserType.ConnectOptions().setHeaders(Map.of("x-api-key", KEY)));
    try {
      Page page = browser.newPage();
      // chrome.runtime / browser.runtime exist in a page only when an extension talks to it.
      out.addProperty("extension_api", (Boolean) page.evaluate(s.api()));
      page.close();

      // The Playwright equivalents, in one context.
      BrowserContext context = browser.newContext();
      context.addInitScript(CONTENT_SCRIPT); // content script
      context.setExtraHTTPHeaders(Map.of("x-my-extension", "on")); // header rewriting
      int[] blocked = {0}; // request blocking, like an ad or image blocker
      context.route(Pattern.compile("\\.(png|jpe?g|gif|webp)(\\?|$)"), route -> { blocked[0]++; route.abort(); });
      Page p = context.newPage();
      int injected = 0;
      for (String url : List.of(SITE, SITE + "catalogue/page-2.html")) {
        p.navigate(url, new Page.NavigateOptions().setTimeout(60000));
        if (p.locator("#my-extension-banner").count() > 0) injected++;
      }
      out.addProperty("content_script_pages", injected);
      out.addProperty("blocked_images", blocked[0]);
      // httpbin echoes the request headers; read the response body, not the rendered page.
      Response resp = p.navigate("https://httpbin.org/headers", new Page.NavigateOptions().setTimeout(60000));
      JsonObject headers = JsonParser.parseString(resp.text()).getAsJsonObject().getAsJsonObject("headers");
      out.addProperty("header_added", headers.has("X-My-Extension") && headers.get("X-My-Extension").getAsString().equals("on"));
    } finally {
      browser.close();
    }
    return out;
  }

  public static void main(String[] args) throws Exception {
    JsonArray results = new JsonArray();
    try (Playwright pw = Playwright.create()) {
      for (Setup s : SETUPS) results.add(one(pw, s));
    }
    JsonObject result = new JsonObject();
    result.add("results", results);
    System.out.println(GSON.toJson(result));
  }
}
