// Maven: com.microsoft.playwright:playwright:1.60.0, com.google.code.gson:gson:2.11.0
// Run with PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1.
// env: CDPFLEET_API_KEY, PROXY_URL
import com.google.gson.*;
import com.microsoft.playwright.*;
import com.microsoft.playwright.options.Media;
import java.net.URI;
import java.net.http.*;
import java.nio.charset.StandardCharsets;
import java.util.*;
import java.util.regex.*;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");
  static final Gson GSON = new GsonBuilder().setPrettyPrinting().disableHtmlEscaping().create();

  // Which browsers can print a page to PDF over a remote connection, headless and headful.
  record Setup(String label, String engine, String family, JsonPrimitive headless) {}
  static final Setup[] SETUPS = {
    new Setup("Chromium, headless", "chromium", "chromium", new JsonPrimitive(true)),
    new Setup("Chrome, headless", "chrome", "chromium", new JsonPrimitive("new")),
    new Setup("Chrome, headful", "chrome", "chromium", new JsonPrimitive(false)),
    new Setup("Edge, headful", "edge", "chromium", new JsonPrimitive(false)),
    new Setup("Firefox, headful", "firefox", "firefox", new JsonPrimitive(false)),
    new Setup("Camoufox, headful", "camoufox", "firefox", new JsonPrimitive(false)),
    new Setup("WebKit, headful", "webkit", "webkit", new JsonPrimitive(false)),
  };

  // A page with a coloured background, a screen-only banner hidden by print CSS and enough text
  // for several printed pages. setContent: no proxy traffic, the same page everywhere.
  static String html() {
    StringBuilder paras = new StringBuilder();
    for (int i = 1; i <= 120; i++) paras.append("<p>Paragraph ").append(i).append(": lorem ipsum dolor sit amet, consectetur adipiscing elit.</p>");
    return "<html><head><style>"
        + "body { font: 14px sans-serif; background: #e8f0ff; }"
        + ".screen-only { background: #c00; color: #fff; padding: 8px; }"
        + "@media print { .screen-only { display: none; } }"
        + "</style></head><body><div class=\"screen-only\">Cookie banner (screen only)</div><h1>Quarterly report</h1>"
        + paras + "</body></html>";
  }

  static int pagesIn(byte[] pdf) {
    Matcher m = Pattern.compile("/Type\\s*/Page(?!s)").matcher(new String(pdf, StandardCharsets.ISO_8859_1));
    int n = 0;
    while (m.find()) n++;
    return n;
  }

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

  static JsonObject one(Playwright pw, Setup s) throws Exception {
    JsonObject body = new JsonObject();
    body.addProperty("proxy", System.getenv("PROXY_URL"));
    body.add("headless", s.headless());
    JsonObject session = launch(s.engine(), body);
    BrowserType type = switch (s.family()) { case "firefox" -> pw.firefox(); case "webkit" -> pw.webkit(); default -> pw.chromium(); };
    Browser browser = type.connect(session.get("wsUrl").getAsString(), new BrowserType.ConnectOptions().setHeaders(Map.of("x-api-key", KEY)));
    JsonObject out = new JsonObject();
    out.addProperty("label", s.label());
    try {
      Page page = browser.newPage();
      page.setContent(html());
      long t = System.currentTimeMillis();
      try {
        byte[] pdf = page.pdf(new Page.PdfOptions().setFormat("A4"));
        byte[] withBg = page.pdf(new Page.PdfOptions().setFormat("A4").setPrintBackground(true));
        out.addProperty("pdf", "yes");
        out.addProperty("pdf_bytes", pdf.length);
        out.addProperty("pdf_pages", pagesIn(pdf));
        out.addProperty("with_background_bytes", withBg.length);
        out.addProperty("pdf_seconds", Math.round((System.currentTimeMillis() - t) / 10.0) / 100.0);
      } catch (PlaywrightException err) { // Firefox, Camoufox and WebKit refuse
        // Java wraps the message: Error { message='page.pdf: …' … }
        Matcher m = Pattern.compile("message='([^'\n]*)").matcher(err.getMessage());
        String msg = m.find() ? m.group(1) : err.getMessage().split("\n")[0];
        out.addProperty("pdf", "no");
        out.addProperty("error", msg.substring(0, Math.min(120, msg.length())));
      }
      // The fallback that works everywhere: print CSS + a full-page screenshot.
      page.emulateMedia(new Page.EmulateMediaOptions().setMedia(Media.PRINT));
      out.addProperty("print_css_applied", (Boolean) page.evaluate("() => getComputedStyle(document.querySelector('.screen-only')).display === 'none'"));
      out.addProperty("print_screenshot_kb", Math.round(page.screenshot(new Page.ScreenshotOptions().setFullPage(true)).length / 1024.0));
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
