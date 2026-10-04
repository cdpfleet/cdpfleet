// Maven: com.microsoft.playwright:playwright:1.60.0, com.google.code.gson:gson:2.11.0
// Run with PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1. env: CDPFLEET_API_KEY, PROXY_URL
import com.google.gson.*;
import com.microsoft.playwright.*;
import com.microsoft.playwright.options.AriaRole;
import java.net.URI;
import java.net.http.*;
import java.nio.charset.StandardCharsets;
import java.nio.file.*;
import java.util.*;
import java.util.zip.*;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");

  static JsonObject row(String artifact, Long bytes, String detail, Integer screenshots, Integer snapshots, Integer network, String openWith) {
    JsonObject r = new JsonObject();
    r.addProperty("artifact", artifact);
    r.add("bytes", bytes == null ? JsonNull.INSTANCE : new JsonPrimitive(bytes));
    r.addProperty("detail", detail);
    r.add("screenshots", screenshots == null ? JsonNull.INSTANCE : new JsonPrimitive(screenshots));
    r.add("snapshots", snapshots == null ? JsonNull.INSTANCE : new JsonPrimitive(snapshots));
    r.add("network_entries", network == null ? JsonNull.INSTANCE : new JsonPrimitive(network));
    r.add("open_with", openWith == null ? JsonNull.INSTANCE : new JsonPrimitive(openWith));
    return r;
  }

  public static void main(String[] args) throws Exception {
    Path dir = Files.createTempDirectory("cdpfleet-replay-");
    HttpClient http = HttpClient.newHttpClient();
    String body = "{\"proxy\": " + new Gson().toJson(System.getenv("PROXY_URL")) + ", \"headless\": \"new\"}";
    HttpResponse<String> res = http.send(HttpRequest.newBuilder(URI.create("https://starter.cdpfleet.com/chromium/session"))
        .header("x-api-key", KEY).header("content-type", "application/json")
        .POST(HttpRequest.BodyPublishers.ofString(body)).build(), HttpResponse.BodyHandlers.ofString());
    if (res.statusCode() != 200) throw new RuntimeException("launch: " + res.statusCode() + " " + res.body());
    String wsUrl = JsonParser.parseString(res.body()).getAsJsonObject().get("wsUrl").getAsString();

    long t = System.currentTimeMillis();
    try (Playwright playwright = Playwright.create()) {
      Browser browser = playwright.chromium().connect(wsUrl, new BrowserType.ConnectOptions().setHeaders(Map.of("x-api-key", KEY)));
      try {
        // Video is recorded on the remote browser and fetched when the page closes; the trace is
        // assembled by the client from events and screenshots streamed over the same connection.
        BrowserContext context = browser.newContext(new Browser.NewContextOptions()
            .setRecordVideoDir(dir).setRecordVideoSize(1280, 720).setViewportSize(1280, 720));
        context.tracing().start(new Tracing.StartOptions().setScreenshots(true).setSnapshots(true));
        Page page = context.newPage();
        Video video = page.video(); // take the handle while the page is open
        page.navigate("https://example.com/", new Page.NavigateOptions().setTimeout(60000));
        page.navigate("https://httpbin.org/forms/post", new Page.NavigateOptions().setTimeout(60000));
        page.getByLabel("Customer name").fill("Ada Lovelace");
        page.getByLabel("Large").check();
        page.getByRole(AriaRole.BUTTON, new Page.GetByRoleOptions().setName("Submit order")).click();
        page.waitForURL("**/post", new Page.WaitForURLOptions().setTimeout(60000)); // httpbin echoes the form as JSON
        page.screenshot(new Page.ScreenshotOptions().setPath(dir.resolve("final.png")));
        context.tracing().stop(new Tracing.StopOptions().setPath(dir.resolve("trace.zip")));
        context.close(); // finishes the video
        video.saveAs(dir.resolve("session.webm"));
      } finally {
        browser.close();
      }
    }
    double seconds = (System.currentTimeMillis() - t) / 1000.0;

    List<String> actions = new ArrayList<>();
    int screenshots = 0, snapshots = 0, network = 0;
    try (ZipFile zip = new ZipFile(dir.resolve("trace.zip").toFile())) {
      for (ZipEntry e : Collections.list(zip.entries())) {
        String name = e.getName();
        if (name.startsWith("resources/") && name.endsWith(".jpeg")) screenshots++;
        if (!name.endsWith(".trace") && !name.endsWith(".network")) continue;
        String text = new String(zip.getInputStream(e).readAllBytes(), StandardCharsets.UTF_8);
        for (String line : text.split("\n")) {
          if (line.isBlank()) continue;
          if (name.endsWith(".network")) { network++; continue; }
          JsonObject ev = JsonParser.parseString(line).getAsJsonObject();
          String type = ev.has("type") ? ev.get("type").getAsString() : "";
          if (type.equals("frame-snapshot")) snapshots++;
          if (type.equals("before") && ev.has("method")) actions.add(ev.get("method").getAsString());
        }
      }
    }
    byte[] webm = Files.readAllBytes(dir.resolve("session.webm"));
    boolean isWebm = webm.length >= 4 && (webm[0] & 0xff) == 0x1a && (webm[1] & 0xff) == 0x45 && (webm[2] & 0xff) == 0xdf && (webm[3] & 0xff) == 0xa3;

    JsonArray out = new JsonArray();
    out.add(row("trace.zip", Files.size(dir.resolve("trace.zip")), actions.size() + " actions: " + String.join(", ", actions),
        screenshots, snapshots, network, "npx playwright show-trace trace.zip"));
    out.add(row("session.webm", (long) webm.length, isWebm ? "valid WebM (EBML header)" : "not a WebM file", null, null, null, "any video player"));
    out.add(row("final.png", Files.size(dir.resolve("final.png")), "screenshot after the last step", null, null, null, "image viewer"));
    out.add(row("whole run", null, String.format(Locale.ROOT, "%.1f s including recording and downloads", seconds), null, null, null, null));
    System.out.println(new GsonBuilder().setPrettyPrinting().disableHtmlEscaping().serializeNulls().create().toJson(out));
  }
}
