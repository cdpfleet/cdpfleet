// Maven: com.microsoft.playwright:playwright:1.60.0, com.google.code.gson:gson:2.11.0
// Run with PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1. env: CDPFLEET_API_KEY, PROXY_URL
import com.google.gson.*;
import com.microsoft.playwright.*;
import java.net.URI;
import java.net.http.*;
import java.util.*;
import java.util.concurrent.*;
import java.util.concurrent.atomic.*;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");
  static final HttpClient HTTP = HttpClient.newHttpClient();
  static final int PAGES = 24;

  static class Stats {
    final AtomicInteger launches = new AtomicInteger(), pageRetries = new AtomicInteger(), connectFailures = new AtomicInteger();
    final AtomicLong launchMs = new AtomicLong(), connectMs = new AtomicLong(), sessionMs = new AtomicLong();
    final Map<String, Integer> retries = new ConcurrentHashMap<>();
    final List<String> titles = Collections.synchronizedList(new ArrayList<>());
  }

  // Launch with the retries the API asks for: 429 (thread limit, launch rate) and 503
  // (fleet momentarily busy) carry Retry-After.
  static String launch(Stats stats) throws Exception {
    String body = "{\"proxy\": " + new Gson().toJson(System.getenv("PROXY_URL")) + ", \"headless\": true}";
    for (int attempt = 1; ; attempt++) {
      long t = System.currentTimeMillis();
      HttpResponse<String> res = HTTP.send(HttpRequest.newBuilder(URI.create("https://starter.cdpfleet.com/chromium/session"))
          .header("x-api-key", KEY).header("content-type", "application/json")
          .POST(HttpRequest.BodyPublishers.ofString(body)).build(), HttpResponse.BodyHandlers.ofString());
      JsonObject json = JsonParser.parseString(res.body()).getAsJsonObject();
      if (res.statusCode() == 200) {
        stats.launches.incrementAndGet();
        stats.launchMs.addAndGet(System.currentTimeMillis() - t);
        return json.get("wsUrl").getAsString();
      }
      String error = json.has("error") ? json.get("error").getAsString() : "";
      boolean retryable = res.statusCode() == 503 || (res.statusCode() == 429 && !error.contains("quota"));
      if (!retryable || attempt == 10) throw new RuntimeException("launch: " + res.statusCode() + " " + error);
      stats.retries.merge(error, 1, Integer::sum);
      Thread.sleep(Long.parseLong(res.headers().firstValue("retry-after").orElse("2")) * 1000);
    }
  }

  // Residential proxies drop a tunnel now and then (ERR_TUNNEL_CONNECTION_FAILED): retry.
  static String scrape(Page page, Stats stats) {
    for (int attempt = 1; ; attempt++) {
      try {
        page.navigate("https://en.wikipedia.org/wiki/Special:Random", new Page.NavigateOptions().setTimeout(30000));
        return page.title();
      } catch (PlaywrightException err) {
        stats.pageRetries.incrementAndGet();
        if (attempt == 3) return "(failed: " + err.getMessage().split("\n")[0] + ")";
      }
    }
  }

  // One worker thread. Playwright objects are per thread, so each worker has its own.
  static void worker(boolean reuse, AtomicInteger next, Stats stats) throws Exception {
    try (Playwright playwright = Playwright.create()) {
      Browser browser = null;
      long started = 0;
      Page page = null;
      while (next.getAndIncrement() < PAGES) {
        if (browser == null) {
          // If the connect fails (rare: the server holding the browser didn't answer), don't
          // reconnect to the same wsUrl — launch a fresh session. Such sessions aren't billed.
          for (int attempt = 1; browser == null; attempt++) {
            String wsUrl = launch(stats);
            started = System.currentTimeMillis();
            try {
              browser = playwright.chromium().connect(wsUrl, new BrowserType.ConnectOptions().setHeaders(Map.of("x-api-key", KEY)));
            } catch (PlaywrightException err) {
              stats.connectFailures.incrementAndGet();
              if (attempt == 3) throw err;
            }
          }
          stats.connectMs.addAndGet(System.currentTimeMillis() - started);
          page = browser.newPage();
        }
        try {
          stats.titles.add(scrape(page, stats));
        } finally {
          if (!reuse) {
            browser.close();
            stats.sessionMs.addAndGet(System.currentTimeMillis() - started);
            browser = null;
          }
        }
      }
      if (browser != null) {
        browser.close();
        stats.sessionMs.addAndGet(System.currentTimeMillis() - started);
      }
    }
  }

  // Runs PAGES pages on `workers` parallel workers; each worker either opens one session
  // and reuses it, or opens a new session for every page.
  static JsonObject run(int workers, boolean reuse) throws Exception {
    Stats stats = new Stats();
    AtomicInteger next = new AtomicInteger();
    long t = System.currentTimeMillis();
    ExecutorService pool = Executors.newFixedThreadPool(workers);
    List<Future<?>> futures = new ArrayList<>();
    for (int i = 0; i < workers; i++) futures.add(pool.submit(() -> { worker(reuse, next, stats); return null; }));
    for (Future<?> f : futures) f.get();
    pool.shutdown();
    Gson gson = new Gson();
    JsonObject out = new JsonObject();
    out.addProperty("strategy", reuse ? "reuse one session per worker" : "new session per page");
    out.addProperty("pages", stats.titles.size());
    out.addProperty("wall_seconds", Math.round((System.currentTimeMillis() - t) / 100.0) / 10.0);
    out.addProperty("launches", stats.launches.get());
    out.addProperty("avg_launch_ms", stats.launchMs.get() / stats.launches.get());
    out.addProperty("avg_connect_ms", stats.connectMs.get() / stats.launches.get());
    out.add("launch_retries", gson.toJsonTree(stats.retries));
    out.addProperty("connect_failures", stats.connectFailures.get());
    out.addProperty("page_retries", stats.pageRetries.get());
    out.addProperty("billed_thread_seconds", Math.round(stats.sessionMs.get() / 1000.0));
    out.add("sample_titles", gson.toJsonTree(stats.titles.subList(0, Math.min(3, stats.titles.size()))));
    return out;
  }

  public static void main(String[] args) throws Exception {
    // Size the pool from the plan: never more workers than threads.
    HttpResponse<String> me = HTTP.send(HttpRequest.newBuilder(URI.create("https://cdpfleet.com/v1/me")).header("x-api-key", KEY).build(),
        HttpResponse.BodyHandlers.ofString());
    int threads = JsonParser.parseString(me.body()).getAsJsonObject().getAsJsonObject("subscription").get("threads").getAsInt();
    int workers = Math.min(threads, 6);
    JsonObject out = new JsonObject();
    out.addProperty("plan_threads", threads);
    out.addProperty("workers", workers);
    JsonArray results = new JsonArray();
    results.add(run(workers, false));
    results.add(run(workers, true));
    out.add("results", results);
    System.out.println(new GsonBuilder().setPrettyPrinting().disableHtmlEscaping().create().toJson(out));
  }
}
