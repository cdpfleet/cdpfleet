// Maven: com.microsoft.playwright:playwright:1.60.0, com.google.code.gson:gson:2.11.0
// Run with PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1. env: CDPFLEET_API_KEY, PROXY_URL
import com.google.gson.*;
import com.microsoft.playwright.*;
import java.net.URI;
import java.net.http.*;
import java.nio.charset.StandardCharsets;
import java.nio.file.*;
import java.security.*;
import java.util.*;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");
  static final Path TMP = Paths.get(System.getProperty("java.io.tmpdir"));

  static String sha256(byte[] b) throws Exception {
    return HexFormat.of().formatHex(MessageDigest.getInstance("SHA-256").digest(b));
  }

  // A small page on httpbin.org's origin with an upload form and a download link.
  static final String PAGE = """
      <form method="post" action="/anything" enctype="multipart/form-data">
        <input type="file" name="upload" id="file"><button id="send">Send</button></form>
      <a id="data" href="/bytes/102400?seed=42" download="data.bin">data</a>""";

  public static void main(String[] args) throws Exception {
    // A local file to upload: 2,000 CSV rows (~60 KB).
    Path uploadPath = TMP.resolve("cdpfleet-upload.csv");
    StringBuilder csv = new StringBuilder("id,token\n");
    SecureRandom rnd = new SecureRandom();
    for (int i = 1; i <= 2000; i++) {
      byte[] token = new byte[12];
      rnd.nextBytes(token);
      csv.append(i).append(',').append(HexFormat.of().formatHex(token)).append('\n');
    }
    Files.writeString(uploadPath, csv);
    byte[] local = Files.readAllBytes(uploadPath);

    HttpClient http = HttpClient.newHttpClient();
    String body = "{\"proxy\": " + new Gson().toJson(System.getenv("PROXY_URL")) + ", \"headless\": true}";
    HttpResponse<String> res = http.send(HttpRequest.newBuilder(URI.create("https://starter.cdpfleet.com/chromium/session"))
        .header("x-api-key", KEY).header("content-type", "application/json")
        .POST(HttpRequest.BodyPublishers.ofString(body)).build(), HttpResponse.BodyHandlers.ofString());
    if (res.statusCode() != 200) throw new RuntimeException("launch: " + res.statusCode() + " " + res.body());
    String wsUrl = JsonParser.parseString(res.body()).getAsJsonObject().get("wsUrl").getAsString();

    try (Playwright playwright = Playwright.create()) {
      Browser browser = playwright.chromium().connect(wsUrl, new BrowserType.ConnectOptions().setHeaders(Map.of("x-api-key", KEY)));
      try {
        Page page = browser.newPage();
        page.route("https://httpbin.org/files-demo", route -> route.fulfill(new Route.FulfillOptions().setContentType("text/html").setBody(PAGE)));
        page.navigate("https://httpbin.org/files-demo", new Page.NavigateOptions().setTimeout(60000));

        // Upload: setInputFiles reads the file HERE and streams it to the remote browser.
        long t = System.currentTimeMillis();
        page.setInputFiles("#file", uploadPath);
        Response answer = page.waitForNavigation(new Page.WaitForNavigationOptions().setTimeout(60000), () -> page.click("#send"));
        byte[] echoed = JsonParser.parseString(answer.text()).getAsJsonObject().getAsJsonObject("files").get("upload").getAsString()
            .getBytes(StandardCharsets.UTF_8);
        long uploadMs = System.currentTimeMillis() - t;

        // Download: the file lands on the remote server; saveAs streams it back here.
        page.navigate("https://httpbin.org/files-demo", new Page.NavigateOptions().setTimeout(60000));
        t = System.currentTimeMillis();
        Download download = page.waitForDownload(new Page.WaitForDownloadOptions().setTimeout(60000), () -> page.click("#data"));
        Path downloadPath = TMP.resolve(download.suggestedFilename());
        download.saveAs(downloadPath);
        long downloadMs = System.currentTimeMillis() - t;
        byte[] got = Files.readAllBytes(downloadPath);
        // The same seeded bytes fetched directly from here, to prove the copy is exact.
        byte[] direct = http.send(HttpRequest.newBuilder(URI.create("https://httpbin.org/bytes/102400?seed=42")).build(),
            HttpResponse.BodyHandlers.ofByteArray()).body();

        JsonObject up = new JsonObject();
        up.addProperty("local_file", uploadPath.getFileName().toString());
        up.addProperty("bytes", local.length);
        up.addProperty("sha256", sha256(local));
        up.addProperty("server_received_bytes", echoed.length);
        up.addProperty("server_sha256", sha256(echoed));
        up.addProperty("identical", sha256(local).equals(sha256(echoed)));
        up.addProperty("ms", uploadMs);
        JsonObject down = new JsonObject();
        down.addProperty("suggested_filename", download.suggestedFilename());
        down.addProperty("bytes", got.length);
        down.addProperty("sha256", sha256(got));
        down.addProperty("direct_sha256", sha256(direct));
        down.addProperty("identical", sha256(got).equals(sha256(direct)));
        down.addProperty("ms", downloadMs);
        JsonObject out = new JsonObject();
        out.add("upload", up);
        out.add("download", down);
        System.out.println(new GsonBuilder().setPrettyPrinting().disableHtmlEscaping().create().toJson(out));
      } finally {
        browser.close();
      }
    }
  }
}
