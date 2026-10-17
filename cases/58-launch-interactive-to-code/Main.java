// Maven: com.microsoft.playwright:playwright:1.60.0, com.google.code.gson:gson:2.11.0
// Run with PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1.
// env: CDPFLEET_API_KEY, PROXY_URL (your own proxy; a few KB of traffic)
import com.google.gson.*;
import com.microsoft.playwright.*;
import java.net.URI;
import java.net.URLEncoder;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.net.http.WebSocket;
import java.nio.ByteBuffer;
import java.nio.charset.StandardCharsets;
import java.time.Instant;
import java.util.*;
import java.util.concurrent.*;
import java.util.concurrent.atomic.AtomicInteger;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");
  static final String PROXY = System.getenv("PROXY_URL");
  static final Gson GSON = new GsonBuilder().setPrettyPrinting().disableHtmlEscaping().serializeNulls().create();
  static final HttpClient HTTP = HttpClient.newHttpClient();

  static JsonElement post(String url, JsonObject body) throws Exception {
    HttpResponse<String> res = HTTP.send(HttpRequest.newBuilder(URI.create(url))
        .header("x-api-key", KEY).header("content-type", "application/json")
        .POST(HttpRequest.BodyPublishers.ofString(body.toString())).build(), HttpResponse.BodyHandlers.ofString());
    if (res.statusCode() != 200) throw new RuntimeException(url + " " + res.statusCode() + " " + res.body());
    return JsonParser.parseString(res.body());
  }

  // 503 means the fleet is momentarily busy: wait Retry-After and try again.
  static JsonObject launch(String engine, JsonObject body) throws Exception {
    for (int attempt = 1; ; attempt++) {
      HttpResponse<String> res = HTTP.send(HttpRequest.newBuilder(URI.create("https://starter.cdpfleet.com/" + engine + "/session"))
          .header("x-api-key", KEY).header("content-type", "application/json")
          .POST(HttpRequest.BodyPublishers.ofString(body.toString())).build(), HttpResponse.BodyHandlers.ofString());
      if (res.statusCode() == 200) return JsonParser.parseString(res.body()).getAsJsonObject();
      if (res.statusCode() != 503 || attempt == 5) throw new RuntimeException("launch " + res.statusCode() + " " + res.body());
      Thread.sleep(Long.parseLong(res.headers().firstValue("retry-after").orElse("5")) * 1000);
    }
  }

  // The account API's session history: how long each session ran and why it ended.
  static JsonElement history(String sessionId) throws Exception {
    String since = URLEncoder.encode(Instant.now().minusSeconds(3600).toString(), StandardCharsets.UTF_8);
    for (int i = 0; i < 30; i++) {
      HttpResponse<String> res = HTTP.send(HttpRequest.newBuilder(URI.create("https://cdpfleet.com/v1/me/sessions?limit=50&from=" + since))
          .header("x-api-key", KEY).build(), HttpResponse.BodyHandlers.ofString());
      for (JsonElement e : JsonParser.parseString(res.body()).getAsJsonObject().getAsJsonArray("sessions")) {
        JsonObject row = e.getAsJsonObject();
        if (row.get("session_id").getAsString().equals(sessionId) && row.has("ended_at") && !row.get("ended_at").isJsonNull()) {
          JsonObject out = new JsonObject();
          long ms = Instant.parse(row.get("ended_at").getAsString()).toEpochMilli() - Instant.parse(row.get("started_at").getAsString()).toEpochMilli();
          out.addProperty("seconds", Math.round(ms / 1000.0));
          out.addProperty("end_reason", row.get("end_reason").getAsString());
          return out;
        }
      }
      Thread.sleep(2000);
    }
    return JsonNull.INSTANCE;
  }

  // The viewer: JPEG frames (binary) and JSON messages; "pages" says which URL is on screen.
  static final List<String> SEEN = new CopyOnWriteArrayList<>();
  static final AtomicInteger FRAMES = new AtomicInteger();

  static class Viewer implements WebSocket.Listener {
    final StringBuilder text = new StringBuilder();

    public CompletionStage<?> onBinary(WebSocket ws, ByteBuffer data, boolean last) {
      if (last) FRAMES.incrementAndGet();
      ws.request(1);
      return null;
    }

    public void onError(WebSocket ws, Throwable error) {
      System.err.println("viewer: " + error);
    }

    public CompletionStage<?> onText(WebSocket ws, CharSequence data, boolean last) {
      text.append(data);
      if (last) {
        JsonObject m = JsonParser.parseString(text.toString()).getAsJsonObject();
        text.setLength(0);
        if (m.has("type") && m.get("type").getAsString().equals("pages")) {
          JsonElement current = m.get("current");
          for (JsonElement e : m.getAsJsonArray("pages")) {
            JsonObject pg = e.getAsJsonObject();
            if (pg.get("targetId").equals(current) && pg.has("url") && !pg.get("url").isJsonNull()) {
              String url = pg.get("url").getAsString();
              if (SEEN.isEmpty() || !SEEN.get(SEEN.size() - 1).equals(url)) SEEN.add(url);
            }
          }
        }
      }
      ws.request(1);
      return null;
    }
  }

  static boolean waitForUrl(String part) throws Exception {
    for (int i = 0; i < 60; i++) {
      if (SEEN.stream().anyMatch(u -> u.contains(part))) return true;
      Thread.sleep(500);
    }
    return SEEN.stream().anyMatch(u -> u.contains(part));
  }

  static void send(WebSocket ws, JsonObject m) {
    ws.sendText(m.toString(), true).join();
  }

  static void key(WebSocket ws, String k) {
    for (String action : List.of("keyDown", "keyUp")) {
      JsonObject m = new JsonObject();
      m.addProperty("type", "key");
      m.addProperty("action", action);
      m.addProperty("key", k);
      m.addProperty("code", k);
      send(ws, m);
    }
  }

  public static void main(String[] args) throws Exception {
    // 1. Launch with a start page and no client: the browser opens it by itself. A twin session
    //    with the same options gets no viewer at all, to compare with.
    JsonObject body = new JsonObject();
    body.addProperty("proxy", PROXY);
    body.addProperty("headless", true);
    body.addProperty("live_view", true);
    body.addProperty("record", false);
    body.addProperty("start_url", "https://httpbin.org/");
    body.addProperty("inactivity_timeout", "30s");
    JsonObject session = launch("chromium", body);
    JsonObject twin = launch("chromium", body);
    String wsUrl = session.get("wsUrl").getAsString();

    // 2. A live link in control mode — what "Launch interactive" in the dashboard opens for you.
    JsonObject req = new JsonObject();
    req.addProperty("session_id", session.get("sessionId").getAsString());
    req.addProperty("mode", "control");
    JsonObject live = post("https://" + URI.create(wsUrl).getHost() + "/admin/session/live", req).getAsJsonObject();
    WebSocket viewer = HTTP.newWebSocketBuilder().buildAsync(URI.create(live.get("live_url").getAsString()), new Viewer()).join();

    // 3. The person (here a scripted stand-in): goes to the form from the address bar, then fills
    //    the first three fields from the keyboard — Tab to a field, type.
    boolean startShown = waitForUrl("httpbin.org");
    JsonObject nav = new JsonObject();
    nav.addProperty("type", "navigate");
    nav.addProperty("url", "https://httpbin.org/forms/post");
    send(viewer, nav);
    boolean formShown = waitForUrl("/forms/post");
    Thread.sleep(1500);
    for (String text : List.of("Ada Lovelace", "+44 20 7946 0000", "ada@example.com")) {
      key(viewer, "Tab");
      Thread.sleep(200);
      JsonObject t = new JsonObject();
      t.addProperty("type", "text");
      t.addProperty("text", text);
      send(viewer, t);
      Thread.sleep(300);
    }
    // 4. Then only watches — no input for 45 s, past the 30 s inactivity_timeout.
    Thread.sleep(45000);

    JsonObject out = new JsonObject();
    try (Playwright pw = Playwright.create()) {
      Map<String, String> headers = Map.of("x-api-key", KEY);
      // The unwatched twin has had no input and no viewer for as long: it should be gone.
      boolean twinAlive = true;
      try {
        pw.chromium().connect(twin.get("wsUrl").getAsString(), new BrowserType.ConnectOptions().setHeaders(headers).setTimeout(15000)).close();
      } catch (PlaywrightException e) {
        twinAlive = false;
      }

      // 5. Your code joins the same browser and finds the person's page in the default context.
      Browser browser = pw.chromium().connect(wsUrl, new BrowserType.ConnectOptions().setHeaders(headers));
      out.addProperty("live_protocol", live.get("protocol").getAsString());
      out.addProperty("start_page_shown", startShown);
      out.addProperty("form_shown", formShown);
      out.addProperty("frames_while_by_hand", FRAMES.get());
      out.addProperty("watched_session_alive_after_45s", browser.isConnected());
      out.addProperty("unwatched_twin_alive_after_45s", twinAlive);
      try {
        List<Page> pages = browser.contexts().get(0).pages();
        Page page = pages.stream().filter(pg -> pg.url().contains("/forms/post")).findFirst().orElseThrow();
        JsonArray found = new JsonArray();
        pages.forEach(pg -> found.add(pg.url()));
        out.add("pages_found", found);
        JsonObject typed = new JsonObject();
        for (String name : List.of("custname", "custtel", "custemail")) typed.addProperty(name, page.inputValue("input[name=" + name + "]"));
        out.add("typed_by_hand", typed);
        // 6. The code finishes the job: the choices a script makes reliably, then submit.
        page.check("input[name=size][value=medium]");
        page.check("input[name=topping][value=cheese]");
        page.fill("input[name=delivery]", "19:30");
        page.waitForNavigation(new Page.WaitForNavigationOptions().setUrl("**/post"), () -> page.click("form button"));
        out.add("submitted", JsonParser.parseString(page.textContent("body")).getAsJsonObject().get("form"));
        Thread.sleep(1500);
        JsonArray saw = new JsonArray();
        SEEN.forEach(saw::add);
        out.add("viewer_saw", saw);
      } finally {
        viewer.sendClose(WebSocket.NORMAL_CLOSURE, "").join();
        browser.close();
      }
    }

    // 7. The account API's session history.
    out.add("watched_session", history(session.get("sessionId").getAsString()));
    out.add("unwatched_twin", history(twin.get("sessionId").getAsString()));
    System.out.println(GSON.toJson(out));
  }
}
