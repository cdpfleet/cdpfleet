// Maven: com.microsoft.playwright:playwright:1.60.0, com.google.code.gson:gson:2.11.0
// Run with PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1. env: CDPFLEET_API_KEY, PROXY_URL
import com.google.gson.*;
import com.microsoft.playwright.*;
import com.microsoft.playwright.options.WaitUntilState;
import java.net.URI;
import java.net.http.*;
import java.time.LocalDate;
import java.util.*;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");
  static final String API = "https://cdpfleet.com";
  static final HttpClient HTTP = HttpClient.newHttpClient();
  static final Gson GSON = new GsonBuilder().serializeNulls().setPrettyPrinting().disableHtmlEscaping().create();
  static final int POOL = 4;

  static JsonObject get(String path) throws Exception {
    HttpResponse<String> r = HTTP.send(HttpRequest.newBuilder(URI.create(API + path)).header("x-api-key", KEY).build(), HttpResponse.BodyHandlers.ofString());
    return JsonParser.parseString(r.body()).getAsJsonObject();
  }

  static String[] launch() throws Exception {
    String body = "{\"proxy\": " + GSON.toJson(System.getenv("PROXY_URL")) + ", \"headless\": \"new\", \"record\": false}";
    HttpResponse<String> res = HTTP.send(HttpRequest.newBuilder(URI.create("https://starter.cdpfleet.com/chromium/session"))
        .header("x-api-key", KEY).header("content-type", "application/json")
        .POST(HttpRequest.BodyPublishers.ofString(body)).build(), HttpResponse.BodyHandlers.ofString());
    if (res.statusCode() != 200) throw new RuntimeException("launch: " + res.statusCode() + " " + res.body());
    JsonObject s = JsonParser.parseString(res.body()).getAsJsonObject();
    return new String[] { s.get("sessionId").getAsString(), s.get("wsUrl").getAsString() };
  }

  // How a session ended, from the account API (it appears a few seconds after the end).
  static String endReason(String id) throws Exception {
    for (int i = 0; i < 30; i++) {
      for (JsonElement e : get("/v1/me/sessions?limit=50").getAsJsonArray("sessions")) {
        JsonObject s = e.getAsJsonObject();
        if (s.get("session_id").getAsString().equals(id) && s.has("ended_at") && !s.get("ended_at").isJsonNull()) return s.get("end_reason").getAsString();
      }
      Thread.sleep(3000);
    }
    return null;
  }

  static boolean load(Page page, String url) {
    for (int attempt = 1; ; attempt++) {
      try { page.navigate(url, new Page.NavigateOptions().setWaitUntil(WaitUntilState.DOMCONTENTLOADED).setTimeout(45000)); return true; }
      catch (PlaywrightException e) { if (attempt == 2) return false; }
    }
  }

  public static void main(String[] args) throws Exception {
    // Twelve light, public catalogue pages: what a crawler works through.
    List<String> urls = new ArrayList<>();
    for (int i = 1; i <= 12; i++) urls.add("https://books.toscrape.com/catalogue/page-" + i + ".html");
    JsonObject sub = get("/v1/me").getAsJsonObject("subscription");
    int maxPages = sub.getAsJsonObject("features").get("max_pages").getAsInt();
    int memoryMb = sub.getAsJsonObject("features").get("memory_mb").getAsInt();
    JsonObject plan = new JsonObject();
    plan.addProperty("kind", sub.get("kind").getAsString()); plan.addProperty("max_pages", maxPages); plan.addProperty("memory_mb", memoryMb);

    JsonObject tabCap = new JsonObject(), pool = new JsonObject(), memory = new JsonObject();
    double heapMb;
    try (Playwright pw = Playwright.create()) {
      Map<String, String> headers = Map.of("x-api-key", KEY);
      // 1 + 2: tabs. A new tab per URL, never closed, runs into the tab cap; a small pool of reused
      // tabs works through the same list without ever getting near it.
      String[] t = launch();
      Browser tabs = pw.chromium().connect(t[1], new BrowserType.ConnectOptions().setHeaders(headers));
      try {
        BrowserContext ctx = tabs.newContext();
        List<Page> opened = new ArrayList<>();
        Integer closedAt = null;
        for (int i = 0; i <= maxPages; i++) {
          // Over the cap the node closes the new tab at once: newPage() fails, or the tab is already closed.
          Page p = null;
          try { p = ctx.newPage(); Thread.sleep(1500); } catch (PlaywrightException e) { }
          if (p == null || p.isClosed()) { closedAt = i + 1; break; }
          p.setContent("<title>tab " + (i + 1) + "</title>");
          opened.add(p);
        }
        boolean alive = tabs.isConnected() && opened.get(0).title().equals("tab 1");
        tabCap.addProperty("opened_before_cap", opened.size()); tabCap.addProperty("closed_at_tab", closedAt); tabCap.addProperty("session_alive", alive);
        for (Page p : opened) p.close();

        List<Page> pages = new ArrayList<>();
        for (int i = 0; i < POOL; i++) pages.add(ctx.newPage());
        int loaded = 0;
        // Java's Playwright is single-threaded: the pool loads one page at a time and keeps the tab count fixed.
        for (int i = 0; i < urls.size(); i++) if (load(pages.get(i % POOL), urls.get(i))) loaded++;
        double sum = 0;
        for (Page p : pages) sum += ((Number) p.evaluate("() => (performance.memory && performance.memory.usedJSHeapSize) || 0")).doubleValue();
        heapMb = Math.round(sum / pages.size() / 1048576 * 10) / 10.0;
        pool.addProperty("pool_size", POOL); pool.addProperty("urls", urls.size()); pool.addProperty("loaded", loaded); pool.addProperty("open_tabs", ctx.pages().size());
        for (Page p : pages) p.close();
      } finally { tabs.close(); }

      // 3: memory. Half the cap is fine; growing a page past the cap ends the session.
      String[] m = launch();
      Browser mem = pw.chromium().connect(m[1], new BrowserType.ConnectOptions().setHeaders(headers));
      int mb = 0; boolean halfOk = false, cut = false;
      try {
        Page page = mem.newPage();
        page.evaluate("() => { window.hog = []; }");
        try {
          while (mb < memoryMb + 1024) {
            page.evaluate("() => { const a = new Uint8Array(128 << 20); for (let i = 0; i < a.length; i += 4096) a[i] = 1; window.hog.push(a); }");
            mb += 128;
            if (mb >= memoryMb / 2 && !halfOk) halfOk = mem.isConnected();
          }
        } catch (PlaywrightException e) { cut = true; }
      } finally { try { mem.close(); } catch (PlaywrightException e) { } }
      memory.addProperty("half_cap_ok", halfOk); memory.addProperty("cut", cut); memory.addProperty("allocated_mb_at_cut", mb);
      memory.addProperty("end_reason", endReason(m[0]));
    }
    JsonObject out = new JsonObject();
    out.addProperty("measured_at", LocalDate.now().toString());
    out.add("plan", plan); out.add("tab_cap", tabCap); out.add("pool", pool);
    out.addProperty("js_heap_mb_per_tab", heapMb); out.add("memory", memory);
    System.out.println(GSON.toJson(out));
  }
}
