// Maven: com.microsoft.playwright:playwright:1.60.0, com.google.code.gson:gson:2.11.0
// Run with PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1. env: CDPFLEET_API_KEY, PROXY_URL
import com.google.gson.*;
import com.microsoft.playwright.*;
import java.net.URI;
import java.net.http.*;
import java.util.Map;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");

  static final String PAGE_SIGNALS = """
      () => ({
        viewport: `${innerWidth}x${innerHeight}`,
        screen: `${screen.width}x${screen.height}`,
        device_pixel_ratio: devicePixelRatio,
        max_touch_points: navigator.maxTouchPoints,
        coarse_pointer: matchMedia('(pointer: coarse)').matches,
        platform: navigator.platform,
        ua_data_mobile: navigator.userAgentData ? navigator.userAgentData.mobile : null,
        ua_data_platform: navigator.userAgentData ? navigator.userAgentData.platform : null,
      })""";

  // Playwright for Java has no device registry: these are its iPhone 15 Pro and Pixel 7.
  static Browser.NewContextOptions device(String ua, int w, int h, int sw, int sh, double scale) {
    return new Browser.NewContextOptions().setUserAgent(ua).setViewportSize(w, h).setScreenSize(sw, sh)
        .setDeviceScaleFactor(scale).setIsMobile(true).setHasTouch(true);
  }

  // What a page can see about the device, plus what the network sees (tls.peet.ws).
  static JsonObject inspect(BrowserContext context) {
    Page page = context.newPage();
    JsonObject fp = JsonParser.parseString(page.navigate("https://tls.peet.ws/api/all",
        new Page.NavigateOptions().setTimeout(60000)).text()).getAsJsonObject();
    JsonObject js = new Gson().toJsonTree(page.evaluate(PAGE_SIGNALS)).getAsJsonObject();
    JsonArray headers = null;
    for (JsonElement f : fp.getAsJsonObject("http2").getAsJsonArray("sent_frames")) {
      if (f.getAsJsonObject().get("frame_type").getAsString().equals("HEADERS")) headers = f.getAsJsonObject().getAsJsonArray("headers");
    }
    page.close();
    JsonObject out = new JsonObject();
    out.add("user_agent", fp.get("user_agent"));
    for (String k : js.keySet()) out.add(k, js.get(k));
    out.addProperty("sec_ch_ua_mobile", header(headers, "sec-ch-ua-mobile"));
    out.addProperty("sec_ch_ua_platform", header(headers, "sec-ch-ua-platform"));
    out.add("ja4", fp.getAsJsonObject("tls").get("ja4"));
    out.add("akamai_h2_hash", fp.getAsJsonObject("http2").get("akamai_fingerprint_hash"));
    return out;
  }

  static String header(JsonArray headers, String name) {
    for (JsonElement h : headers) if (h.getAsString().startsWith(name + ": ")) return h.getAsString().substring(name.length() + 2);
    return null;
  }

  public static void main(String[] args) throws Exception {
    String body = "{\"proxy\": " + new Gson().toJson(System.getenv("PROXY_URL")) + ", \"headless\": true}";
    HttpResponse<String> res = HttpClient.newHttpClient().send(HttpRequest.newBuilder(URI.create("https://starter.cdpfleet.com/chrome/session"))
        .header("x-api-key", KEY).header("content-type", "application/json")
        .POST(HttpRequest.BodyPublishers.ofString(body)).build(), HttpResponse.BodyHandlers.ofString());
    if (res.statusCode() != 200) throw new RuntimeException("launch: " + res.statusCode() + " " + res.body());
    String wsUrl = JsonParser.parseString(res.body()).getAsJsonObject().get("wsUrl").getAsString();

    try (Playwright playwright = Playwright.create()) {
      Browser browser = playwright.chromium().connect(wsUrl, new BrowserType.ConnectOptions().setHeaders(Map.of("x-api-key", KEY)));
      try {
        JsonObject out = new JsonObject();
        out.add("desktop", inspect(browser.newContext()));
        out.add("iPhone 15 Pro", inspect(browser.newContext(device(
            "Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/26.4 Mobile/15E148 Safari/604.1",
            393, 659, 393, 852, 3))));
        out.add("Pixel 7", inspect(browser.newContext(device(
            "Mozilla/5.0 (Linux; Android 14; Pixel 7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/148.0.7778.96 Mobile Safari/537.36",
            412, 839, 412, 915, 2.625))));
        System.out.println(new GsonBuilder().setPrettyPrinting().disableHtmlEscaping().create().toJson(out));
      } finally {
        browser.close();
      }
    }
  }
}
