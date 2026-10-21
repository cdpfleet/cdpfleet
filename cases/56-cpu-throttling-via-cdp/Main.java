// Maven: com.microsoft.playwright:playwright:1.60.0, com.google.code.gson:gson:2.11.0
// Run with PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1. env: CDPFLEET_API_KEY, PROXY_URL
import com.google.gson.*;
import com.microsoft.playwright.*;
import com.microsoft.playwright.options.WaitUntilState;
import java.net.URI;
import java.net.http.*;
import java.util.*;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");
  static final int[] RATES = {1, 4, 6};
  static final String URL = "https://en.wikipedia.org/wiki/Web_browser";
  // A fixed amount of JavaScript work, timed inside the page: what a heavy client-side app does.
  static final String WORK = """
      (() => {
        const t = performance.now();
        let h = 0;
        for (let i = 0; i < 3e6; i++) h = (h * 31 + i) % 1000003;
        const a = Array.from({ length: 200000 }, (_, i) => (i * 7919) % 100003).sort((x, y) => x - y);
        return JSON.stringify({ ms: Math.round(performance.now() - t), check: h + a[1000] });
      })()""";
  static final String NAV = """
      JSON.stringify((() => {
        const n = performance.getEntriesByType('navigation')[0];
        return { response_ms: Math.round(n.responseEnd), dcl_ms: Math.round(n.domContentLoadedEventEnd), load_ms: Math.round(n.loadEventEnd) };
      })())""";

  static JsonObject json(Object s) { return JsonParser.parseString((String) s).getAsJsonObject(); }

  static JsonObject rate(int rate) {
    JsonObject o = new JsonObject();
    o.addProperty("rate", rate);
    return o;
  }

  public static void main(String[] args) throws Exception {
    Gson gson = new GsonBuilder().serializeNulls().create();
    String body = "{\"proxy\": " + gson.toJson(System.getenv("PROXY_URL")) + ", \"headless\": \"new\"}";
    HttpResponse<String> res = HttpClient.newHttpClient().send(HttpRequest.newBuilder(URI.create("https://starter.cdpfleet.com/chromium/session"))
        .header("x-api-key", KEY).header("content-type", "application/json")
        .POST(HttpRequest.BodyPublishers.ofString(body)).build(), HttpResponse.BodyHandlers.ofString());
    if (res.statusCode() != 200) throw new RuntimeException("launch: " + res.statusCode() + " " + res.body());
    String wsUrl = JsonParser.parseString(res.body()).getAsJsonObject().get("wsUrl").getAsString();

    try (Playwright playwright = Playwright.create()) {
      Browser browser = playwright.chromium().connect(wsUrl, new BrowserType.ConnectOptions().setHeaders(Map.of("x-api-key", KEY)));
      try {
        JsonArray rows = new JsonArray();
        for (int r : RATES) {
          BrowserContext context = browser.newContext();
          Page page = context.newPage();
          // CPU throttling is a DevTools feature: reach it with a CDP session over the Playwright connection.
          CDPSession cdp = context.newCDPSession(page);
          cdp.send("Performance.enable");
          cdp.send("Emulation.setCPUThrottlingRate", rate(r));
          // A real page load: the network part isn't throttled, the main-thread work is.
          page.navigate(URL, new Page.NavigateOptions().setWaitUntil(WaitUntilState.LOAD).setTimeout(90000));
          JsonObject nav = json(page.evaluate(NAV));
          double taskDuration = 0;
          for (JsonElement m : cdp.send("Performance.getMetrics").getAsJsonArray("metrics"))
            if (m.getAsJsonObject().get("name").getAsString().equals("TaskDuration")) taskDuration = m.getAsJsonObject().get("value").getAsDouble();
          JsonObject work = json(page.evaluate(WORK));
          // Load it once more and keep the shorter parse time: parsing waits for blocking scripts, and one
          // slow download through the proxy can inflate a single load at any rate.
          page.reload(new Page.ReloadOptions().setWaitUntil(WaitUntilState.LOAD).setTimeout(90000));
          JsonObject again = json(page.evaluate(NAV));
          JsonObject row = new JsonObject();
          row.addProperty("rate", r);
          row.add("work_ms", work.get("ms"));
          for (Map.Entry<String, JsonElement> e : nav.entrySet()) row.add(e.getKey(), e.getValue());
          // HTML fully received → DOMContentLoaded: parsing and scripts
          row.addProperty("parse_ms", Math.min(nav.get("dcl_ms").getAsLong() - nav.get("response_ms").getAsLong(),
              again.get("dcl_ms").getAsLong() - again.get("response_ms").getAsLong()));
          row.addProperty("main_thread_ms", Math.round(taskDuration * 1000)); // busy time while loading, from CDP
          row.add("check", work.get("check"));
          rows.add(row);
          context.close();
        }

        // Is it per page? A throttled tab next to an unthrottled one in the same browser.
        BrowserContext context = browser.newContext();
        Page slow = context.newPage();
        Page fast = context.newPage();
        context.newCDPSession(slow).send("Emulation.setCPUThrottlingRate", rate(6));
        JsonObject sideBySide = new JsonObject();
        sideBySide.add("throttled_tab_ms", json(slow.evaluate(WORK)).get("ms"));
        sideBySide.add("other_tab_ms", json(fast.evaluate(WORK)).get("ms"));
        context.close();

        JsonObject out = new JsonObject();
        out.add("rows", rows);
        out.add("side_by_side", sideBySide);
        System.out.println(new GsonBuilder().serializeNulls().setPrettyPrinting().disableHtmlEscaping().create().toJson(out));
      } finally {
        browser.close();
      }
    }
  }
}
