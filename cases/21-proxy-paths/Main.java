// Maven: com.microsoft.playwright:playwright:1.60.0, com.google.code.gson:gson:2.11.0
// Run with PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1. env: CDPFLEET_API_KEY, PROXY_URL
import com.google.gson.*;
import com.microsoft.playwright.*;
import com.microsoft.playwright.options.RequestOptions;
import java.net.URI;
import java.net.http.*;
import java.util.Map;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");
  // Reports the caller's IP, user agent, HTTP version and TLS fingerprint (JA4).
  static final String PEET = "https://tls.peet.ws/api/all";
  static final Gson GSON = new GsonBuilder().setPrettyPrinting().disableHtmlEscaping().serializeNulls().create();
  static final HttpClient HTTP = HttpClient.newHttpClient();

  static JsonObject get(String url) throws Exception {
    HttpResponse<String> res = HTTP.send(HttpRequest.newBuilder(URI.create(url)).GET().build(), HttpResponse.BodyHandlers.ofString());
    return JsonParser.parseString(res.body()).getAsJsonObject();
  }

  static JsonObject row(String path, JsonObject seen, String runsOn) throws Exception {
    String ip = seen.get("ip").getAsString().split(":")[0];
    // Who owns the exit: a residential ISP (your proxy) or a hosting provider (a server)?
    JsonObject who = get("http://ip-api.com/json/" + ip + "?fields=hosting");
    JsonObject tls = seen.getAsJsonObject("tls");
    boolean resumed = false;
    for (JsonElement e : tls.getAsJsonArray("extensions")) {
      JsonElement name = e.getAsJsonObject().get("name");
      if (name != null && !name.isJsonNull() && name.getAsString().contains("pre_shared_key")) resumed = true;
    }
    JsonObject row = new JsonObject();
    row.addProperty("path", path);
    row.addProperty("runs_on", runsOn);
    row.addProperty("exit_ip", ip);
    row.addProperty("exit_type", who.has("hosting") && who.get("hosting").getAsBoolean() ? "datacenter" : "residential");
    row.add("user_agent", seen.get("user_agent"));
    row.add("http_version", seen.get("http_version"));
    row.add("ja4", tls.get("ja4"));
    row.addProperty("tls_extensions", tls.getAsJsonArray("extensions").size());
    row.addProperty("resumed_tls", resumed);
    return row;
  }

  public static void main(String[] args) throws Exception {
    String body = "{\"proxy\": " + GSON.toJson(System.getenv("PROXY_URL")) + ", \"headless\": \"new\"}";
    HttpResponse<String> res = HTTP.send(HttpRequest.newBuilder(URI.create("https://starter.cdpfleet.com/chromium/session"))
        .header("x-api-key", KEY).header("content-type", "application/json")
        .POST(HttpRequest.BodyPublishers.ofString(body)).build(), HttpResponse.BodyHandlers.ofString());
    if (res.statusCode() != 200) throw new RuntimeException("launch " + res.statusCode() + " " + res.body());
    JsonObject session = JsonParser.parseString(res.body()).getAsJsonObject();

    try (Playwright playwright = Playwright.create()) {
      Browser browser = playwright.chromium().connect(session.get("wsUrl").getAsString(),
          new BrowserType.ConnectOptions().setHeaders(Map.of("x-api-key", KEY)));
      try {
        Page page = browser.newPage();
        // 1. A navigation: the browser itself makes the request.
        JsonObject nav = JsonParser.parseString(page.navigate(PEET, new Page.NavigateOptions().setTimeout(60000)).text()).getAsJsonObject();
        // 2. fetch() inside the page (same origin): the browser's network stack, cookies and headers.
        JsonObject inPage = GSON.toJsonTree(page.evaluate("() => fetch('/api/all').then((r) => r.json())")).getAsJsonObject();
        // 3. page.request(): Playwright's own HTTP client, run by the Playwright server next to the browser.
        JsonObject viaRequest = JsonParser.parseString(page.request().get(PEET, RequestOptions.create().setTimeout(60000)).text()).getAsJsonObject();
        // 4. Your own HTTP client on your machine, for reference.
        JsonObject local = get(PEET);

        JsonArray out = new JsonArray();
        out.add(row("page.goto", nav, "the browser"));
        out.add(row("fetch() in page.evaluate", inPage, "the browser"));
        out.add(row("page.request.get", viaRequest, "Playwright server"));
        out.add(row("fetch() in your script", local, "your machine"));
        // JA4's middle part hashes the cipher suites: the same TLS stack keeps it across connections.
        String browserCiphers = out.get(0).getAsJsonObject().get("ja4").getAsString().split("_")[1];
        for (JsonElement e : out) {
          JsonObject r = e.getAsJsonObject();
          r.addProperty("same_tls_stack_as_browser", r.get("ja4").getAsString().split("_")[1].equals(browserCiphers));
        }
        System.out.println(GSON.toJson(out));
      } finally {
        browser.close();
      }
    }
  }
}
