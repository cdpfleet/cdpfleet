// Maven: com.microsoft.playwright:playwright:1.60.0, com.google.code.gson:gson:2.11.0
// Run with PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1.
// env: CDPFLEET_API_KEY (your plan), PROXY_URL; optional COMMUNITY_API_KEY, CDPFLEET_DEDICATED_API_KEY to compare plans
import com.google.gson.*;
import com.microsoft.playwright.*;
import java.net.URI;
import java.net.http.*;
import java.time.LocalDate;
import java.util.*;
import java.util.regex.*;

public class Main {
  static final String STARTER = "https://starter.cdpfleet.com";
  static final String API = "https://cdpfleet.com";
  static final String PAGE = "https://en.wikipedia.org/wiki/World_War_II"; // a long article: a mid-weight page
  static final int MAX_TABS = 50; // the per-session tab cap on every plan
  static final String PROXY = System.getenv("PROXY_URL");
  static final HttpClient HTTP = HttpClient.newHttpClient();
  static final Gson GSON = new GsonBuilder().serializeNulls().setPrettyPrinting().disableHtmlEscaping().create();

  // Each run: an account, an engine and a memory multiplier. Runs whose key isn't set are skipped.
  static final Object[][] RUNS = {
    { "COMMUNITY_API_KEY", "chromium", 1 },
    { "CDPFLEET_API_KEY", "chromium", 1 },
    { "CDPFLEET_API_KEY", "firefox", 1 },
    { "CDPFLEET_API_KEY", "webkit", 1 },
    { "CDPFLEET_API_KEY", "chromium", 2 },
    { "CDPFLEET_DEDICATED_API_KEY", "chromium", 1 },
  };

  static JsonObject api(String key, String path) throws Exception {
    HttpRequest req = HttpRequest.newBuilder(URI.create(API + path)).header("x-api-key", key).build();
    return JsonParser.parseString(HTTP.send(req, HttpResponse.BodyHandlers.ofString()).body()).getAsJsonObject();
  }

  static String firstLine(Exception e) { return String.valueOf(e.getMessage()).split("\n")[0]; }

  static JsonObject run(Playwright pw, String env, String engine, int memory) throws Exception {
    String key = System.getenv(env);
    JsonObject sub = api(key, "/v1/me").getAsJsonObject("subscription");
    JsonElement features = sub.get("features");
    boolean community = features != null && features.isJsonObject() && features.getAsJsonObject().has("community") && features.getAsJsonObject().get("community").getAsBoolean();
    String plan = community ? "Community" : "dedicated".equals(sub.get("kind").getAsString()) ? "Dedicated" : "Shared";
    long t0 = System.currentTimeMillis();
    // Community launches use the plan's included proxy; paid plans bring their own.
    JsonObject body = new JsonObject();
    body.addProperty("headless", true);
    if (memory > 1) body.addProperty("memory", memory);
    if (!community) body.addProperty("proxy", PROXY);
    HttpRequest launchReq = HttpRequest.newBuilder(URI.create(STARTER + "/" + engine + "/session"))
      .header("x-api-key", key).header("content-type", "application/json")
      .POST(HttpRequest.BodyPublishers.ofString(body.toString())).build();
    HttpResponse<String> res = HTTP.send(launchReq, HttpResponse.BodyHandlers.ofString());
    JsonObject out = new JsonObject();
    out.addProperty("plan", plan);
    out.addProperty("engine", engine);
    out.addProperty("memory", memory);
    if (res.statusCode() != 200) { out.addProperty("launch_error", res.statusCode() + " " + res.body()); return out; }
    JsonObject launch = JsonParser.parseString(res.body()).getAsJsonObject();
    BrowserType type = engine.equals("firefox") ? pw.firefox() : engine.equals("webkit") ? pw.webkit() : pw.chromium();
    Browser browser = type.connect(launch.get("wsUrl").getAsString(), new BrowserType.ConnectOptions().setHeaders(Map.of("x-api-key", key)));
    BrowserContext context = browser.newContext();
    // Open the article in one new tab after another until the session ends or the tab cap is reached.
    int tabs = 0;
    String clientError = null;
    while (tabs < MAX_TABS) {
      try {
        Page page = context.newPage();
        try {
          page.navigate(PAGE, new Page.NavigateOptions().setTimeout(60000));
        } catch (TimeoutError e) { // a slow load through a shared proxy gets one more try; a closed session doesn't
          page.navigate(PAGE, new Page.NavigateOptions().setTimeout(60000));
        }
        tabs++;
      } catch (Exception e) {
        clientError = firstLine(e);
        break;
      }
    }
    // At the cap with memory to spare: what does a 51st tab get?
    String tab51 = null;
    if (tabs == MAX_TABS) {
      try { context.newPage(); tab51 = "opened"; } catch (Exception e) { tab51 = firstLine(e); }
    }
    try { browser.close(); } catch (Exception ignored) { }
    // The session's record: why it ended, its weight, peak memory and time.
    JsonObject record = null;
    for (int i = 0; i < 20 && (record == null || record.get("ended_at").isJsonNull()); i++) {
      Thread.sleep(1500);
      for (JsonElement s : api(key, "/v1/me/sessions?limit=20").getAsJsonArray("sessions"))
        if (s.getAsJsonObject().get("session_id").getAsString().equals(launch.get("sessionId").getAsString())) record = s.getAsJsonObject();
    }
    if (clientError != null) {
      Matcher m = Pattern.compile("(Session closed: .*)$").matcher(clientError);
      if (m.find()) clientError = m.group(1);
    }
    out.add("weight", launch.get("weight"));
    out.add("memory_mb", launch.get("memory_mb"));
    out.addProperty("tabs", tabs);
    out.addProperty("client_error", clientError);
    out.addProperty("tab_51", tab51);
    out.add("end_reason", record != null ? record.get("end_reason") : JsonNull.INSTANCE);
    out.add("memory_peak_mb", record != null ? record.get("memory_peak_mb") : JsonNull.INSTANCE);
    out.addProperty("seconds", record != null ? record.get("duration_seconds").getAsInt() : (int) ((System.currentTimeMillis() - t0) / 1000));
    if (record != null) out.addProperty("thread_seconds", record.get("duration_seconds").getAsInt() * record.get("weight").getAsInt());
    else out.add("thread_seconds", JsonNull.INSTANCE);
    return out;
  }

  public static void main(String[] args) throws Exception {
    JsonArray runs = new JsonArray();
    try (Playwright pw = Playwright.create()) {
      for (Object[] r : RUNS) // one at a time: one site, one session at a time
        if (System.getenv((String) r[0]) != null) runs.add(run(pw, (String) r[0], (String) r[1], (Integer) r[2]));
    }
    JsonObject out = new JsonObject();
    out.addProperty("language", "java");
    out.addProperty("page", PAGE);
    out.addProperty("max_tabs", MAX_TABS);
    out.addProperty("measured_at", LocalDate.now().toString());
    out.add("runs", runs);
    System.out.println(GSON.toJson(out));
  }
}
