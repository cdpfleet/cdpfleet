// Maven: com.microsoft.playwright:playwright:1.60.0, com.google.code.gson:gson:2.11.0
// Run with PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1.
// env: CDPFLEET_API_KEY, PROXY_URL (any exit), PROXY_URL_DE (an exit in Germany)
import com.google.gson.*;
import com.microsoft.playwright.*;
import java.net.URI;
import java.net.http.*;
import java.util.Map;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");

  // A German Windows desktop: every value below is part of one consistent story.
  static String persona(String proxy) {
    return """
        {
          "proxy": %s,
          "headless": true,
          "os": "windows",
          "locale": "de-DE",
          "screen": {"minWidth": 1920, "maxWidth": 1920, "minHeight": 1080, "maxHeight": 1080},
          "window": [1600, 900],
          "humanize": true,
          "block_webrtc": true,
          "geoip": true
        }""".formatted(new Gson().toJson(proxy)); // geoip: timezone and location follow the exit IP
  }

  static final String PAGE_SIGNALS = """
      () => {
        const gl = document.createElement('canvas').getContext('webgl');
        const dbg = gl && gl.getExtension('WEBGL_debug_renderer_info');
        return {
          platform: navigator.platform,
          languages: navigator.languages,
          timezone: Intl.DateTimeFormat().resolvedOptions().timeZone,
          screen: `${screen.width}x${screen.height}`,
          window: `${outerWidth}x${outerHeight}`,
          hardware_concurrency: navigator.hardwareConcurrency,
          webgl_renderer: dbg ? gl.getParameter(dbg.UNMASKED_RENDERER_WEBGL) : null,
          webrtc: typeof RTCPeerConnection !== 'undefined',
        };
      }""";

  static JsonObject run(Playwright playwright, String proxy) throws Exception {
    HttpResponse<String> res = HttpClient.newHttpClient().send(HttpRequest.newBuilder(URI.create("https://starter.cdpfleet.com/camoufox/session"))
        .header("x-api-key", KEY).header("content-type", "application/json")
        .POST(HttpRequest.BodyPublishers.ofString(persona(proxy))).build(), HttpResponse.BodyHandlers.ofString());
    if (res.statusCode() != 200) throw new RuntimeException("launch: " + res.statusCode() + " " + res.body());
    String wsUrl = JsonParser.parseString(res.body()).getAsJsonObject().get("wsUrl").getAsString();
    Browser browser = playwright.firefox().connect(wsUrl, new BrowserType.ConnectOptions().setHeaders(Map.of("x-api-key", KEY)));
    try {
      Page page = browser.newPage();
      JsonObject fp = JsonParser.parseString(page.navigate("https://tls.peet.ws/api/all",
          new Page.NavigateOptions().setTimeout(60000)).text()).getAsJsonObject();
      JsonObject seen = new Gson().toJsonTree(page.evaluate(PAGE_SIGNALS)).getAsJsonObject();
      String acceptLanguage = null;
      for (JsonElement f : fp.getAsJsonObject("http2").getAsJsonArray("sent_frames")) {
        if (!f.getAsJsonObject().get("frame_type").getAsString().equals("HEADERS")) continue;
        for (JsonElement h : f.getAsJsonObject().getAsJsonArray("headers")) {
          if (h.getAsString().startsWith("accept-language: ")) acceptLanguage = h.getAsString().substring(17);
        }
      }
      // Where the proxy exits, as a website would look it up.
      JsonObject geo = JsonParser.parseString(page.navigate("http://ip-api.com/json/?fields=country,timezone",
          new Page.NavigateOptions().setTimeout(60000)).text()).getAsJsonObject();
      JsonObject out = new JsonObject();
      out.add("exit_country", geo.get("country"));
      out.add("exit_timezone", geo.get("timezone"));
      out.add("user_agent", fp.get("user_agent"));
      out.addProperty("accept_language", acceptLanguage);
      for (String k : seen.keySet()) out.add(k, seen.get(k));
      out.add("ja4", fp.getAsJsonObject("tls").get("ja4"));
      return out;
    } finally {
      browser.close();
    }
  }

  public static void main(String[] args) throws Exception {
    try (Playwright playwright = Playwright.create()) {
      JsonObject out = new JsonObject();
      out.add("random exit", run(playwright, System.getenv("PROXY_URL")));
      out.add("German exit", run(playwright, System.getenv("PROXY_URL_DE")));
      System.out.println(new GsonBuilder().setPrettyPrinting().disableHtmlEscaping().create().toJson(out));
    }
  }
}
