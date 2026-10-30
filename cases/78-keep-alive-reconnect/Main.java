// Maven: com.microsoft.playwright:playwright:1.60.0, com.google.code.gson:gson:2.11.0
// Run with PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1.
// env: CDPFLEET_DEDICATED_API_KEY (keep_alive needs dedicated threads), PROXY_URL
import com.google.gson.*;
import com.microsoft.playwright.*;
import com.microsoft.playwright.options.Cookie;
import java.net.URI;
import java.net.http.*;
import java.time.LocalDate;
import java.util.*;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_DEDICATED_API_KEY");
  static final String PROXY = System.getenv("PROXY_URL");
  static final String STARTER = "https://starter.cdpfleet.com";
  static final String API = "https://cdpfleet.com";
  static final int IDLE_S = 30; // inactivity_timeout: how long the session waits for a client
  static final String SITE = "https://books.toscrape.com";
  static final HttpClient HTTP = HttpClient.newHttpClient();
  static final Gson GSON = new GsonBuilder().serializeNulls().setPrettyPrinting().disableHtmlEscaping().create();
  static final Map<String, String> HEADERS = Map.of("x-api-key", KEY);

  static JsonObject api(String path) throws Exception {
    HttpRequest req = HttpRequest.newBuilder(URI.create(API + path)).header("x-api-key", KEY).build();
    return JsonParser.parseString(HTTP.send(req, HttpResponse.BodyHandlers.ofString()).body()).getAsJsonObject();
  }

  // The session as the account API sees it while it runs: listed in /v1/me/threads or not.
  static JsonObject listed(String sid) throws Exception {
    for (JsonElement t : api("/v1/me/threads").getAsJsonArray("threads"))
      if (t.getAsJsonObject().get("session_id").getAsString().equals(sid)) return t.getAsJsonObject();
    return null;
  }

  // Today's thread-seconds on the account: { closed, live }.
  static int[] usage() throws Exception {
    JsonObject u = api("/v1/me/usage");
    return new int[] { u.get("closed_thread_seconds").getAsInt(), u.get("live_thread_seconds").getAsInt() };
  }

  // A shared proxy drops the odd connection: one retry.
  static void go(Page page, String url) {
    try { page.navigate(url, new Page.NavigateOptions().setTimeout(45000)); }
    catch (PlaywrightException e) { page.navigate(url, new Page.NavigateOptions().setTimeout(45000)); }
  }

  static double secs(long a, long b) { return Math.round((b - a) / 100.0) / 10.0; }

  public static void main(String[] args) throws Exception {
    int closedBefore = usage()[0];

    // 1. Launch with keep_alive: a disconnect no longer ends the session.
    long t0 = System.currentTimeMillis();
    JsonObject body = new JsonObject();
    body.addProperty("proxy", PROXY);
    body.addProperty("headless", "new");
    body.addProperty("keep_alive", true);
    body.addProperty("inactivity_timeout", IDLE_S + "s");
    HttpRequest launch = HttpRequest.newBuilder(URI.create(STARTER + "/chromium/session"))
      .header("x-api-key", KEY).header("content-type", "application/json")
      .POST(HttpRequest.BodyPublishers.ofString(body.toString())).build();
    HttpResponse<String> res = HTTP.send(launch, HttpResponse.BodyHandlers.ofString());
    if (res.statusCode() != 200) throw new RuntimeException("launch failed: " + res.statusCode() + " " + res.body());
    JsonObject session = JsonParser.parseString(res.body()).getAsJsonObject();
    String sid = session.get("sessionId").getAsString();
    String wsUrl = session.get("wsUrl").getAsString();

    JsonObject out = new JsonObject();
    out.addProperty("language", "java");
    out.addProperty("inactivity_timeout_s", IDLE_S);
    try (Playwright pw = Playwright.create()) {
      BrowserType.ConnectOptions opts = new BrowserType.ConnectOptions().setHeaders(HEADERS);
      // 2. First client: an agent works through the catalogue in one context, a second context on the side.
      Browser browser = pw.chromium().connect(wsUrl, opts);
      BrowserContext work = browser.newContext();
      Page page = work.newPage();
      go(page, SITE + "/catalogue/page-1.html");
      page.click("li.next a"); // step 2 of the run: page 2
      work.addCookies(List.of(new Cookie("agent_step", "2").setDomain("books.toscrape.com").setPath("/")));
      page.evaluate("() => localStorage.setItem('agent_state', JSON.stringify({ seen: 40, next: 'page-3' }))");
      go(work.newPage(), SITE + "/catalogue/a-light-in-the-attic_1000/index.html");
      BrowserContext side = browser.newContext();
      go(side.newPage(), SITE + "/catalogue/category/books/poetry_23/index.html");
      JsonObject before = new JsonObject();
      before.addProperty("contexts", browser.contexts().size());
      before.addProperty("pages", browser.contexts().stream().mapToInt(c -> c.pages().size()).sum());
      before.addProperty("url", page.url());
      out.add("before", before);

      // 3. The client goes away. With keep_alive, close() only disconnects.
      browser.close();
      long tDetach = System.currentTimeMillis();
      Thread.sleep(3000);
      JsonObject whileDetached = listed(sid);
      Thread.sleep(12000);

      // 4. A new client reconnects to the same wsUrl and carries on where the first one stopped.
      long tReconnect = System.currentTimeMillis();
      browser = pw.chromium().connect(wsUrl, opts);
      long reconnectMs = System.currentTimeMillis() - tReconnect;
      JsonObject detached = new JsonObject();
      detached.addProperty("listed", whileDetached != null);
      detached.add("running_seconds", whileDetached == null ? JsonNull.INSTANCE : whileDetached.get("running_seconds"));
      detached.addProperty("seconds", secs(tDetach, tReconnect));
      out.add("detached", detached);

      List<BrowserContext> contexts = browser.contexts();
      List<String> urls = new ArrayList<>();
      BrowserContext backCtx = null;
      Page back = null;
      int pages = 0;
      for (BrowserContext c : contexts) for (Page p : c.pages()) {
        pages++;
        urls.add(p.url().replace(SITE, ""));
        if (p.url().contains("page-2.html")) { backCtx = c; back = p; }
      }
      Collections.sort(urls);
      JsonObject reconnect = new JsonObject();
      reconnect.addProperty("ms", reconnectMs);
      reconnect.addProperty("contexts", contexts.size());
      reconnect.addProperty("pages", pages);
      reconnect.add("urls", GSON.toJsonTree(urls));
      String cookie = null, local = null, continued = null;
      if (back != null) {
        for (Cookie c : backCtx.cookies(SITE)) if (c.name.equals("agent_step")) cookie = c.value;
        local = (String) back.evaluate("() => localStorage.getItem('agent_state')");
        back.click("li.next a");
        back.waitForURL("**/page-3.html");
        continued = back.url().replace(SITE, "");
      }
      reconnect.addProperty("cookie", cookie);
      reconnect.addProperty("local_storage", local);
      reconnect.addProperty("continued", continued);
      out.add("reconnect", reconnect);

      // 5. The client leaves for good: the session waits inactivity_timeout, then ends.
      browser.close();
      long tLeave = System.currentTimeMillis();
      Double goneAfter = null;
      Integer liveWaiting = null;
      while (System.currentTimeMillis() - tLeave < (IDLE_S + 90) * 1000L) {
        Thread.sleep(2000);
        if (liveWaiting == null && System.currentTimeMillis() - tLeave >= 20000) liveWaiting = usage()[1];
        if (listed(sid) == null) { goneAfter = secs(tLeave, System.currentTimeMillis()); break; }
      }
      JsonObject late = new JsonObject();
      try {
        Browser b = pw.chromium().connect(wsUrl, new BrowserType.ConnectOptions().setHeaders(HEADERS).setTimeout(20000));
        late.addProperty("connected", true);
        late.addProperty("contexts", b.contexts().size());
        b.close();
      } catch (PlaywrightException e) {
        String msg = e.getMessage().split("\n")[0];
        late.addProperty("connected", false);
        late.addProperty("error", msg.substring(0, Math.min(160, msg.length())));
      }

      // 6. What was billed: the session's record in the account history.
      Thread.sleep(5000);
      JsonObject s = new JsonObject();
      for (JsonElement x : api("/v1/me/sessions?limit=20").getAsJsonArray("sessions"))
        if (x.getAsJsonObject().get("session_id").getAsString().equals(sid)) s = x.getAsJsonObject();
      int closedAfter = usage()[0];
      JsonObject end = new JsonObject();
      end.addProperty("gone_after_s", goneAfter);
      end.add("late_reconnect", late);
      end.add("end_reason", s.has("end_reason") ? s.get("end_reason") : JsonNull.INSTANCE);
      end.add("duration_seconds", s.has("duration_seconds") ? s.get("duration_seconds") : JsonNull.INSTANCE);
      end.add("weight", s.has("weight") ? s.get("weight") : JsonNull.INSTANCE);
      end.addProperty("client_seconds", secs(t0, tLeave));
      out.add("end", end);
      JsonObject use = new JsonObject();
      use.addProperty("live_while_waiting", liveWaiting);
      use.addProperty("billed", closedAfter - closedBefore);
      out.add("usage", use);
    }
    out.addProperty("measured_at", LocalDate.now().toString());
    System.out.println(GSON.toJson(out));
  }
}
