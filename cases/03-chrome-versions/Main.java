// Maven: com.microsoft.playwright:playwright:1.60.0, com.google.code.gson:gson:2.11.0
// Run with PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1. env: CDPFLEET_API_KEY, PROXY_URL
import com.google.gson.*;
import com.microsoft.playwright.*;
import java.net.URI;
import java.net.http.*;
import java.util.*;
import java.util.concurrent.*;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");
  static final HttpClient HTTP = HttpClient.newHttpClient();

  static JsonObject probe(String label, JsonObject options, String expected) throws Exception {
    // headless: false (a real display) so the user agent doesn't say "HeadlessChrome".
    options.addProperty("proxy", System.getenv("PROXY_URL"));
    options.addProperty("headless", false);
    HttpResponse<String> res = HTTP.send(HttpRequest.newBuilder(URI.create("https://starter.cdpfleet.com/chrome/session"))
        .header("x-api-key", KEY).header("content-type", "application/json")
        .POST(HttpRequest.BodyPublishers.ofString(options.toString())).build(), HttpResponse.BodyHandlers.ofString());
    JsonObject out = new JsonObject();
    out.addProperty("variant", label);
    if (res.statusCode() != 200) { out.addProperty("error", res.statusCode() + " " + res.body()); return out; }
    String wsUrl = JsonParser.parseString(res.body()).getAsJsonObject().get("wsUrl").getAsString();
    // Playwright objects are per thread: each probe gets its own instance.
    try (Playwright playwright = Playwright.create()) {
      Browser browser = playwright.chromium().connect(wsUrl, new BrowserType.ConnectOptions().setHeaders(Map.of("x-api-key", KEY)));
      try {
        Response nav;
        try {
          nav = browser.newPage().navigate("https://tls.peet.ws/api/all", new Page.NavigateOptions().setTimeout(30000));
        } catch (PlaywrightException err) { // a residential exit occasionally times out: one retry, in a fresh tab
          nav = browser.newPage().navigate("https://tls.peet.ws/api/all", new Page.NavigateOptions().setTimeout(30000));
        }
        JsonObject fp = JsonParser.parseString(nav.text()).getAsJsonObject();
        String secChUa = null;
        for (JsonElement f : fp.getAsJsonObject("http2").getAsJsonArray("sent_frames")) {
          if (!f.getAsJsonObject().get("frame_type").getAsString().equals("HEADERS")) continue;
          for (JsonElement h : f.getAsJsonObject().getAsJsonArray("headers")) {
            if (h.getAsString().startsWith("sec-ch-ua: ")) secChUa = h.getAsString().substring(11);
          }
        }
        out.addProperty("catalog_version", expected);
        out.addProperty("browser_version", browser.version());
        out.add("user_agent", fp.get("user_agent"));
        out.addProperty("sec_ch_ua", secChUa);
        out.add("ja4", fp.getAsJsonObject("tls").get("ja4"));
        out.add("akamai_h2_hash", fp.getAsJsonObject("http2").get("akamai_fingerprint_hash"));
        return out;
      } finally {
        browser.close();
      }
    }
  }

  public static void main(String[] args) throws Exception {
    // The live catalog says which channels and previous majors exist right now.
    JsonObject catalog = JsonParser.parseString(HTTP.send(HttpRequest.newBuilder(URI.create("https://cdpfleet.com/api/public/browsers")).build(),
        HttpResponse.BodyHandlers.ofString()).body()).getAsJsonObject();
    List<Callable<JsonObject>> jobs = new ArrayList<>();
    for (JsonElement e : catalog.getAsJsonArray("engines")) {
      if (!e.getAsJsonObject().get("key").getAsString().equals("chrome")) continue;
      for (JsonElement ve : e.getAsJsonObject().getAsJsonArray("versions")) {
        String labelName = ve.getAsJsonObject().get("label").getAsString();
        String version = ve.getAsJsonObject().get("version").getAsString();
        JsonObject options = new JsonObject();
        String label;
        if (labelName.equals("pinned")) {
          String major = version.split("\\.")[0];
          options.addProperty("version", major);
          label = "version " + major;
        } else {
          if (!labelName.equals("stable")) options.addProperty("channel", labelName);
          label = "channel " + labelName;
        }
        jobs.add(() -> probe(label, options, version));
      }
    }
    // All variants at once: each is its own session.
    ExecutorService pool = Executors.newFixedThreadPool(jobs.size());
    JsonArray rows = new JsonArray();
    for (Future<JsonObject> f : pool.invokeAll(jobs)) rows.add(f.get());
    pool.shutdown();
    System.out.println(new GsonBuilder().setPrettyPrinting().disableHtmlEscaping().create().toJson(rows));
  }
}
