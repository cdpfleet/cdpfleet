// Maven: com.microsoft.playwright:playwright:1.60.0, com.google.code.gson:gson:2.11.0
// Run with PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1. env: CDPFLEET_API_KEY, PROXY_URL
import com.google.gson.*;
import com.microsoft.playwright.*;
import com.microsoft.playwright.options.Proxy;
import com.microsoft.playwright.options.RequestOptions;
import java.net.URI;
import java.net.URLDecoder;
import java.net.http.*;
import java.nio.charset.StandardCharsets;
import java.util.*;
import java.util.regex.*;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");
  static final String UA = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/154.0.0.0 Safari/537.36";
  static final Gson GSON = new GsonBuilder().serializeNulls().setPrettyPrinting().disableHtmlEscaping().create();

  // Three pages, one question each: is the data in the HTML, or does it need JavaScript?
  record Target(String url, String item, int expected) {}
  static final List<Target> TARGETS = List.of(
      new Target("https://books.toscrape.com/", "product_pod", 20),
      new Target("https://quotes.toscrape.com/js/", "quote", 10),
      new Target("https://quotes.toscrape.com/scroll", "quote", 10));

  static int countInHtml(String html, String cls) {
    Matcher m = Pattern.compile("class=\"[^\"]*\\b" + Pattern.quote(cls) + "\\b[^\"]*\"").matcher(html);
    int n = 0;
    while (m.find()) n++;
    return n;
  }

  public static void main(String[] args) throws Exception {
    String proxyUrl = System.getenv("PROXY_URL");
    URI proxy = URI.create(proxyUrl);
    String[] creds = (proxy.getRawUserInfo() == null ? "" : proxy.getRawUserInfo()).split(":", 2);
    String user = URLDecoder.decode(creds[0], StandardCharsets.UTF_8);
    String pass = creds.length > 1 ? URLDecoder.decode(creds[1], StandardCharsets.UTF_8) : "";

    try (Playwright playwright = Playwright.create()) {
      // Step 1: a plain HTTP GET through the same proxy, no browser (Playwright's request API
      // runs locally; the proxy keeps the exit IP identical to the browser's).
      APIRequestContext http = playwright.request().newContext(new APIRequest.NewContextOptions()
          .setProxy(new Proxy(proxy.getScheme() + "://" + proxy.getHost() + ":" + proxy.getPort()).setUsername(user).setPassword(pass))
          .setUserAgent(UA));
      List<JsonObject> rows = new ArrayList<>();
      for (Target t : TARGETS) {
        long t0 = System.currentTimeMillis();
        APIResponse res = http.get(t.url(), RequestOptions.create().setTimeout(60000));
        String html = res.text();
        JsonObject row = new JsonObject();
        row.addProperty("url", t.url());
        row.addProperty("http_status", res.status());
        row.addProperty("html_kb", Math.round(html.length() / 1024.0));
        row.addProperty("items_in_html", countInHtml(html, t.item()));
        row.addProperty("fetch_seconds", (System.currentTimeMillis() - t0) / 1000.0);
        rows.add(row);
      }
      http.dispose();

      // Step 2: only the pages whose HTML didn't have the items get a browser.
      List<JsonObject> needsBrowser = new ArrayList<>();
      for (int i = 0; i < rows.size(); i++) if (rows.get(i).get("items_in_html").getAsInt() < TARGETS.get(i).expected()) needsBrowser.add(rows.get(i));
      if (!needsBrowser.isEmpty()) {
        String body = "{\"proxy\": " + GSON.toJson(proxyUrl) + ", \"headless\": \"new\"}";
        HttpResponse<String> launch = HttpClient.newHttpClient().send(HttpRequest.newBuilder(URI.create("https://starter.cdpfleet.com/chromium/session"))
            .header("x-api-key", KEY).header("content-type", "application/json")
            .POST(HttpRequest.BodyPublishers.ofString(body)).build(), HttpResponse.BodyHandlers.ofString());
        if (launch.statusCode() != 200) throw new RuntimeException("launch: " + launch.statusCode() + " " + launch.body());
        String wsUrl = JsonParser.parseString(launch.body()).getAsJsonObject().get("wsUrl").getAsString();
        Browser browser = playwright.chromium().connect(wsUrl, new BrowserType.ConnectOptions().setHeaders(Map.of("x-api-key", KEY)));
        try {
          for (JsonObject r : needsBrowser) {
            Target t = TARGETS.stream().filter(x -> x.url().equals(r.get("url").getAsString())).findFirst().orElseThrow();
            Page page = browser.newPage();
            long t0 = System.currentTimeMillis();
            page.navigate(r.get("url").getAsString(), new Page.NavigateOptions().setTimeout(60000));
            page.locator("." + t.item()).first().waitFor(new Locator.WaitForOptions().setTimeout(60000));
            r.addProperty("items_in_browser", page.locator("." + t.item()).count());
            r.addProperty("browser_seconds", (System.currentTimeMillis() - t0) / 1000.0);
            page.close();
          }
        } finally {
          browser.close();
        }
      }
      for (JsonObject r : rows) {
        r.addProperty("needs_browser", r.has("items_in_browser"));
        if (!r.has("items_in_browser")) r.add("items_in_browser", JsonNull.INSTANCE);
        if (!r.has("browser_seconds")) r.add("browser_seconds", JsonNull.INSTANCE);
      }
      System.out.println(GSON.toJson(rows));
    }
  }
}
