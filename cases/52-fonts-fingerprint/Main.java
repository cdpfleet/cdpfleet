// Maven: com.microsoft.playwright:playwright:1.60.0, com.google.code.gson:gson:2.11.0
// Run with PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1.
// env: CDPFLEET_API_KEY, PROXY_URL
import com.google.gson.*;
import com.microsoft.playwright.*;
import java.net.URI;
import java.net.http.*;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.util.*;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");
  static final Gson GSON = new GsonBuilder().setPrettyPrinting().disableHtmlEscaping().serializeNulls().create();
  static final HttpClient HTTP = HttpClient.newHttpClient();

  // The families a font-fingerprinting script typically tries: Windows, macOS, Linux and popular extras.
  static final List<String> CANDIDATES = List.of(
      "Arial", "Arial Black", "Calibri", "Cambria", "Candara", "Comic Sans MS", "Consolas", "Constantia", "Corbel", "Courier New",
      "Georgia", "Impact", "Lucida Console", "Lucida Sans Unicode", "Microsoft Sans Serif", "Palatino Linotype", "Segoe UI", "Tahoma",
      "Times New Roman", "Trebuchet MS", "Verdana", "American Typewriter", "Avenir", "Avenir Next", "Futura", "Geneva", "Gill Sans",
      "Helvetica", "Helvetica Neue", "Lucida Grande", "Menlo", "Monaco", "Optima", "SF Pro", "DejaVu Sans", "DejaVu Serif",
      "Liberation Sans", "Liberation Mono", "Noto Sans", "Ubuntu", "Cantarell", "Roboto", "Open Sans", "Fira Code", "JetBrains Mono",
      "Source Code Pro");

  // The classic width probe: a family is "installed" if text set in it, with a generic fallback,
  // measures differently from the generic family alone.
  static final String PROBE = """
      (fonts) => {
        const span = document.createElement('span');
        span.textContent = 'mmmmmmmmmmlli10OWQ@#';
        span.style.cssText = 'position:absolute;left:-9999px;font-size:72px';
        document.body.appendChild(span);
        const size = (family) => { span.style.fontFamily = family; return span.offsetWidth + 'x' + span.offsetHeight; };
        const generics = ['monospace', 'serif', 'sans-serif'];
        const base = {};
        for (const g of generics) base[g] = size(g);
        const found = fonts.filter((f) => generics.some((g) => size('"' + f + '", ' + g) !== base[g]));
        span.remove();
        return { platform: navigator.platform, found, generic_sizes: generics.map((g) => base[g]) };
      }""";

  static final String[][] SETUPS = {
      {"os: windows (launch 1)", "{\"os\":\"windows\"}"},
      {"os: windows (launch 2)", "{\"os\":\"windows\"}"},
      {"os: macos", "{\"os\":\"macos\"}"},
      {"os: linux", "{\"os\":\"linux\"}"},
      {"windows + fonts", "{\"os\":\"windows\",\"fonts\":[\"Menlo\",\"Helvetica Neue\",\"Fira Code\"]}"},
      {"windows + fonts + custom_fonts_only", "{\"os\":\"windows\",\"fonts\":[\"Calibri\",\"Consolas\",\"Segoe UI\",\"Fira Code\"],\"custom_fonts_only\":true}"},
  };

  // 503 means the fleet is momentarily busy: wait Retry-After and try again.
  static JsonObject launch(JsonObject body) throws Exception {
    for (int attempt = 1; ; attempt++) {
      HttpResponse<String> res = HTTP.send(HttpRequest.newBuilder(URI.create("https://starter.cdpfleet.com/camoufox/session"))
          .header("x-api-key", KEY).header("content-type", "application/json")
          .POST(HttpRequest.BodyPublishers.ofString(body.toString())).build(), HttpResponse.BodyHandlers.ofString());
      if (res.statusCode() == 200) return JsonParser.parseString(res.body()).getAsJsonObject();
      if (res.statusCode() != 503 || attempt == 5) throw new RuntimeException("launch " + res.statusCode() + " " + res.body());
      Thread.sleep(Long.parseLong(res.headers().firstValue("retry-after").orElse("5")) * 1000);
    }
  }

  // Proxies drop a tunnel now and then: up to two retries.
  static void go(Page page, String url) throws Exception {
    for (int attempt = 1; ; attempt++) {
      try {
        page.navigate(url, new Page.NavigateOptions().setTimeout(60000));
        return;
      } catch (PlaywrightException e) {
        if (attempt == 3) throw e;
        Thread.sleep(2000);
      }
    }
  }

  public static void main(String[] args) throws Exception {
    JsonArray results = new JsonArray();
    try (Playwright pw = Playwright.create()) {
      for (String[] s : SETUPS) {
        JsonObject body = JsonParser.parseString(s[1]).getAsJsonObject();
        body.addProperty("proxy", System.getenv("PROXY_URL"));
        body.addProperty("headless", true);
        body.addProperty("record", false);
        JsonObject session = launch(body);
        Browser browser = pw.firefox().connect(session.get("wsUrl").getAsString(), new BrowserType.ConnectOptions().setHeaders(Map.of("x-api-key", KEY)));
        try {
          Page page = browser.newContext().newPage();
          go(page, "https://httpbin.org/html");
          JsonObject r = GSON.toJsonTree(page.evaluate(PROBE, CANDIDATES)).getAsJsonObject();
          List<String> found = new ArrayList<>();
          r.getAsJsonArray("found").forEach((f) -> found.add(f.getAsString()));
          Set<String> sizes = new HashSet<>();
          r.getAsJsonArray("generic_sizes").forEach((g) -> sizes.add(g.getAsString()));
          JsonObject out = new JsonObject();
          out.addProperty("setup", s[0]);
          out.addProperty("platform", r.get("platform").getAsString());
          out.addProperty("detected", found.size());
          out.addProperty("font_hash", HexFormat.of().formatHex(MessageDigest.getInstance("SHA-256").digest(String.join(",", found).getBytes(StandardCharsets.UTF_8))).substring(0, 10));
          out.addProperty("generics_identical", sizes.size() == 1);
          JsonArray fonts = new JsonArray();
          found.forEach(fonts::add);
          out.add("fonts", fonts);
          results.add(out);
        } finally {
          browser.close();
        }
      }
    }
    System.out.println(GSON.toJson(results));
  }
}
