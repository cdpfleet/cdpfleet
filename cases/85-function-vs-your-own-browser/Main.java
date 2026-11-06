// Maven: com.microsoft.playwright:playwright:1.60.0, com.google.code.gson:gson:2.11.0
// Run with PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1.
// env: CDPFLEET_API_KEY, PROXY_URL
import com.google.gson.*;
import com.microsoft.playwright.*;
import java.net.URI;
import java.net.URLEncoder;
import java.net.http.*;
import java.nio.charset.StandardCharsets;
import java.time.*;
import java.util.*;
import java.util.regex.*;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");
  static final String PROXY = System.getenv("PROXY_URL"); // the same proxy for the Function calls and the sessions
  static final String API = "https://cdpfleet.com";
  static final String STARTER = "https://starter.cdpfleet.com";
  static final String[][] PAGES = {
    { "example.com", "https://example.com/" },
    { "books.toscrape.com", "https://books.toscrape.com/" },
    { "wikipedia", "https://en.wikipedia.org/wiki/Web_scraping" },
  };
  static final String[] TASKS = { "screenshot", "pdf", "markdown" };
  static final String SINCE = Instant.now().minusSeconds(60).toString();
  static final HttpClient HTTP = HttpClient.newHttpClient();
  static final Gson GSON = new GsonBuilder().serializeNulls().setPrettyPrinting().disableHtmlEscaping().create();

  // The record of a session or a Function call in the account's history (a call's x-call-id is its
  // session id there). Billed thread-seconds = (ended_at - started_at) × weight.
  static JsonObject billed(String id) throws Exception {
    for (int i = 0; i < 40; i++) {
      HttpRequest req = HttpRequest.newBuilder(URI.create(API + "/v1/me/sessions?limit=200&from=" + URLEncoder.encode(SINCE, StandardCharsets.UTF_8)))
        .header("x-api-key", KEY).build();
      JsonObject r = JsonParser.parseString(HTTP.send(req, HttpResponse.BodyHandlers.ofString()).body()).getAsJsonObject();
      for (JsonElement e : r.getAsJsonArray("sessions")) {
        JsonObject s = e.getAsJsonObject();
        if (!s.get("session_id").getAsString().equals(id) || s.get("ended_at").isJsonNull()) continue;
        long ms = Instant.parse(s.get("ended_at").getAsString()).toEpochMilli() - Instant.parse(s.get("started_at").getAsString()).toEpochMilli();
        JsonObject o = new JsonObject();
        o.addProperty("engine", s.get("engine").getAsString());
        o.addProperty("weight", s.get("weight").getAsInt());
        o.addProperty("thread_seconds", Math.round(ms * s.get("weight").getAsInt() / 100.0) / 10.0);
        return o;
      }
      Thread.sleep(2000);
    }
    return null;
  }

  // What a file is: PNG width × height; for a PDF, whether it has fonts (a text PDF), its page objects
  // (0 when they sit in compressed object streams) and the tallest image in it.
  static void describe(String task, byte[] buf, JsonObject out) {
    if (task.equals("screenshot")) {
      out.addProperty("width", java.nio.ByteBuffer.wrap(buf, 16, 4).getInt());
      out.addProperty("height", java.nio.ByteBuffer.wrap(buf, 20, 4).getInt());
    } else if (task.equals("pdf")) {
      String s = new String(buf, StandardCharsets.ISO_8859_1);
      int pages = 0, height = 0;
      for (Matcher m = Pattern.compile("/Type\\s*/Page(?![s\\w])").matcher(s); m.find();) pages++;
      for (Matcher m = Pattern.compile("/Height (\\d+)").matcher(s); m.find();) height = Math.max(height, Integer.parseInt(m.group(1)));
      out.addProperty("text_pdf", Pattern.compile("/Font\\b").matcher(s).find());
      out.addProperty("pages", pages);
      out.addProperty("image_height", height);
    }
  }

  // (a) The Function: one POST, the file in the response. One retry if the call fails.
  static JsonObject viaFunction(String task, String url) throws Exception {
    JsonObject body = new JsonObject();
    body.addProperty("url", url);
    body.addProperty("proxy", PROXY);
    if (task.equals("screenshot")) body.addProperty("full_page", true);
    if (task.equals("pdf")) body.addProperty("format", "A4");
    long t = System.currentTimeMillis();
    List<String> calls = new ArrayList<>();
    HttpResponse<byte[]> res = null;
    for (int attempt = 1; attempt <= 2; attempt++) {
      HttpRequest req = HttpRequest.newBuilder(URI.create(API + "/v1/functions/" + task))
        .header("x-api-key", KEY).header("content-type", "application/json").timeout(Duration.ofSeconds(40))
        .POST(HttpRequest.BodyPublishers.ofString(body.toString())).build();
      res = HTTP.send(req, HttpResponse.BodyHandlers.ofByteArray());
      res.headers().firstValue("x-call-id").ifPresent(calls::add);
      if (res.statusCode() == 200) break;
    }
    long ms = System.currentTimeMillis() - t;
    byte[] buf = res.body();
    List<JsonObject> records = new ArrayList<>();
    for (String id : calls) records.add(billed(id));
    JsonObject last = records.isEmpty() ? null : records.get(records.size() - 1);
    JsonObject out = new JsonObject();
    out.addProperty("status", res.statusCode());
    out.addProperty("attempts", calls.size());
    out.addProperty("ms", ms);
    out.addProperty("bytes", buf.length);
    out.addProperty("content_type", res.headers().firstValue("content-type").orElse(null));
    out.addProperty("partial", res.headers().firstValue("x-cdpfleet-partial").orElse(null));
    out.add("engine", last == null ? JsonNull.INSTANCE : last.get("engine"));
    out.add("weight", last == null ? JsonNull.INSTANCE : last.get("weight"));
    if (records.stream().allMatch(Objects::nonNull))
      out.addProperty("thread_seconds", Math.round(records.stream().mapToDouble(r -> r.get("thread_seconds").getAsDouble()).sum() * 10) / 10.0);
    else out.add("thread_seconds", JsonNull.INSTANCE);
    if (res.statusCode() == 200) {
      describe(task, buf, out);
      if (task.equals("markdown")) {
        String md = new String(buf, StandardCharsets.UTF_8);
        String first = md.split("\n", -1)[0];
        out.addProperty("first_line", first.substring(0, Math.min(80, first.length())));
        out.addProperty("has_source_line", md.contains("\nSource: "));
      }
    }
    return out;
  }

  // (b) Your own session: launch over HTTP, connect with the key header.
  record Launched(String sessionId, Browser browser) {}
  static Launched launch(Playwright pw) throws Exception {
    JsonObject body = new JsonObject();
    body.addProperty("proxy", PROXY);
    body.addProperty("headless", true);
    HttpRequest req = HttpRequest.newBuilder(URI.create(STARTER + "/chromium/session"))
      .header("x-api-key", KEY).header("content-type", "application/json")
      .POST(HttpRequest.BodyPublishers.ofString(body.toString())).build();
    HttpResponse<String> res = HTTP.send(req, HttpResponse.BodyHandlers.ofString());
    if (res.statusCode() != 200) throw new RuntimeException("launch " + res.statusCode() + " " + res.body());
    JsonObject s = JsonParser.parseString(res.body()).getAsJsonObject();
    Browser browser = pw.chromium().connect(s.get("wsUrl").getAsString(), new BrowserType.ConnectOptions().setHeaders(Map.of("x-api-key", KEY)));
    return new Launched(s.get("sessionId").getAsString(), browser);
  }

  static Page open(Browser browser, String url) {
    Page page = browser.newPage(new Browser.NewPageOptions().setViewportSize(1280, 720));
    // A proxy drops the odd connection: one retry.
    try { page.navigate(url, new Page.NavigateOptions().setTimeout(45000)); }
    catch (PlaywrightException e) { page.navigate(url, new Page.NavigateOptions().setTimeout(45000)); }
    return page;
  }

  // Markdown has no Playwright call: you'd add a converter (flexmark, …). Measured here: the HTML you
  // would convert and the page's innerText.
  static JsonObject capture(Page page, String task) {
    JsonObject out = new JsonObject();
    if (task.equals("screenshot") || task.equals("pdf")) {
      byte[] buf = task.equals("screenshot") ? page.screenshot(new Page.ScreenshotOptions().setFullPage(true)) : page.pdf(new Page.PdfOptions().setFormat("A4"));
      out.addProperty("bytes", buf.length);
      describe(task, buf, out);
      return out;
    }
    int html = page.content().getBytes(StandardCharsets.UTF_8).length;
    int text = page.locator("body").innerText().getBytes(StandardCharsets.UTF_8).length;
    out.addProperty("bytes", html);
    out.addProperty("html_bytes", html);
    out.addProperty("text_bytes", text);
    return out;
  }

  static JsonObject viaSession(Playwright pw, String task, String url) throws Exception {
    long t = System.currentTimeMillis();
    Launched l = launch(pw);
    long launchMs = System.currentTimeMillis() - t;
    JsonObject cap;
    try {
      cap = capture(open(l.browser(), url), task);
    } finally {
      l.browser().close(); // closing the connection ends the session
    }
    long ms = System.currentTimeMillis() - t;
    JsonObject rec = billed(l.sessionId());
    JsonObject out = new JsonObject();
    out.addProperty("ms", ms);
    out.addProperty("launch_ms", launchMs);
    out.add("engine", rec == null ? JsonNull.INSTANCE : rec.get("engine"));
    out.add("weight", rec == null ? JsonNull.INSTANCE : rec.get("weight"));
    out.add("thread_seconds", rec == null ? JsonNull.INSTANCE : rec.get("thread_seconds"));
    for (String k : cap.keySet()) out.add(k, cap.get(k));
    return out;
  }

  public static void main(String[] args) throws Exception {
    try (Playwright pw = Playwright.create()) {
      JsonArray runs = new JsonArray();
      for (String[] p : PAGES) {
        for (String task : TASKS) {
          JsonObject run = new JsonObject();
          run.addProperty("page", p[0]);
          run.addProperty("task", task);
          run.add("function", viaFunction(task, p[1]));
          run.add("session", viaSession(pw, task, p[1]));
          runs.add(run);
        }
      }

      // (c) One session for all nine captures: launch once, three pages, three outputs each.
      long t = System.currentTimeMillis();
      Launched l = launch(pw);
      int captures = 0;
      try {
        for (String[] p : PAGES) {
          Page page = open(l.browser(), p[1]);
          for (String task : TASKS) { capture(page, task); captures++; }
          page.close();
        }
      } finally {
        l.browser().close();
      }
      long oneMs = System.currentTimeMillis() - t;
      JsonObject rec = billed(l.sessionId());
      JsonObject one = new JsonObject();
      one.addProperty("captures", captures);
      one.addProperty("ms", oneMs);
      one.add("thread_seconds", rec == null ? JsonNull.INSTANCE : rec.get("thread_seconds"));
      one.add("weight", rec == null ? JsonNull.INSTANCE : rec.get("weight"));

      JsonObject out = new JsonObject();
      out.addProperty("language", "java");
      out.addProperty("proxy", "the same PROXY_URL for every Function call and every session");
      out.add("runs", runs);
      out.add("one_session", one);
      out.addProperty("measured_at", LocalDate.now().toString());
      System.out.println(GSON.toJson(out));
    }
  }
}
