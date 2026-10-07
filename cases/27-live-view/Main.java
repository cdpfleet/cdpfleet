// Maven: com.microsoft.playwright:playwright:1.60.0, com.google.code.gson:gson:2.11.0
// Run with PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1. env: CDPFLEET_API_KEY, PROXY_URL
import com.google.gson.*;
import com.microsoft.playwright.*;
import java.net.URI;
import java.net.http.*;
import java.nio.file.*;
import java.util.*;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");
  static final Gson GSON = new GsonBuilder().setPrettyPrinting().disableHtmlEscaping().serializeNulls().create();
  static final HttpClient HTTP = HttpClient.newHttpClient();

  record Frame(String phase, int bytes, boolean jpeg, int w, int h, byte[] data) {}

  static JsonObject watch(Playwright playwright, String engine, String headless) throws Exception {
    String body = "{\"proxy\": " + GSON.toJson(System.getenv("PROXY_URL")) + ", \"headless\": " + headless + "}";
    HttpResponse<String> res = null;
    for (int attempt = 1; attempt <= 5; attempt++) { // 503 = momentarily no capacity for this browser
      res = HTTP.send(HttpRequest.newBuilder(URI.create("https://starter.cdpfleet.com/" + engine + "/session"))
          .header("x-api-key", KEY).header("content-type", "application/json")
          .POST(HttpRequest.BodyPublishers.ofString(body)).build(), HttpResponse.BodyHandlers.ofString());
      if (res.statusCode() != 503) break;
      Thread.sleep(2000L * attempt);
    }
    if (res.statusCode() != 200) {
      JsonObject err = new JsonObject();
      err.addProperty("browser", engine);
      err.addProperty("error", "launch " + res.statusCode() + " " + res.body());
      return err;
    }
    String wsUrl = JsonParser.parseString(res.body()).getAsJsonObject().get("wsUrl").getAsString();
    BrowserType type = engine.equals("chromium") ? playwright.chromium() : playwright.firefox();
    Browser browser = type.connect(wsUrl, new BrowserType.ConnectOptions().setHeaders(Map.of("x-api-key", KEY)));
    try {
      Page page = browser.newPage();
      List<Frame> frames = Collections.synchronizedList(new ArrayList<>()); // every frame, tagged with its phase
      String[] phase = { "load" };
      // Every frame arrives here as JPEG bytes — this is where a viewer would get it.
      page.screencast().start(new Screencast.StartOptions().setQuality(60).setOnFrame(frame -> {
        byte[] data = frame.data();
        boolean jpeg = data.length > 1 && (data[0] & 0xff) == 0xff && (data[1] & 0xff) == 0xd8;
        frames.add(new Frame(phase[0], data.length, jpeg, frame.viewportWidth(), frame.viewportHeight(), data));
      }));
      for (int attempt = 1; ; attempt++) { // the proxy can drop a tunnel; retry
        try {
          page.navigate("https://en.wikipedia.org/wiki/Web_browser", new Page.NavigateOptions().setTimeout(60000));
          break;
        } catch (PlaywrightException e) {
          if (attempt == 3) throw e;
        }
      }
      phase[0] = "scroll";
      long t = System.currentTimeMillis();
      for (int i = 0; i < 10; i++) {
        page.mouse().wheel(0, 500);
        page.waitForTimeout(400);
      }
      double scrolling = (System.currentTimeMillis() - t) / 1000.0;
      phase[0] = "idle"; // nothing changes on the page now
      page.waitForTimeout(3000);
      page.screencast().stop();

      List<Frame> got;
      synchronized (frames) { got = new ArrayList<>(frames); }
      long scroll = got.stream().filter(f -> f.phase().equals("scroll")).count();
      Frame last = got.isEmpty() ? null : got.get(got.size() - 1);
      if (last != null) Files.write(Path.of(System.getProperty("java.io.tmpdir"), "cdpfleet-live-" + engine + ".jpg"), last.data());
      long totalBytes = got.stream().mapToLong(Frame::bytes).sum();
      JsonObject row = new JsonObject();
      row.addProperty("browser", engine);
      row.addProperty("frames_while_loading", got.stream().filter(f -> f.phase().equals("load")).count());
      row.addProperty("frames_while_scrolling", scroll);
      row.addProperty("fps_while_scrolling", Math.round(scroll / scrolling * 10) / 10.0);
      row.addProperty("frames_in_3s_idle", got.stream().filter(f -> f.phase().equals("idle")).count());
      row.addProperty("avg_kb", Math.round((double) totalBytes / Math.max(got.size(), 1) / 1024));
      row.addProperty("all_jpeg", got.stream().allMatch(Frame::jpeg));
      if (last != null) row.addProperty("viewport", last.w() + "x" + last.h());
      else row.add("viewport", JsonNull.INSTANCE);
      return row;
    } finally {
      browser.close();
    }
  }

  public static void main(String[] args) throws Exception {
    JsonArray out = new JsonArray();
    try (Playwright playwright = Playwright.create()) {
      out.add(watch(playwright, "chromium", "\"new\""));
      out.add(watch(playwright, "firefox", "true"));
    }
    System.out.println(GSON.toJson(out));
  }
}
