// Maven: com.microsoft.playwright:playwright:1.60.0, com.google.code.gson:gson:2.11.0
// Run with PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1.
// env: CDPFLEET_API_KEY (one session at a time; public test pages only; nothing is solved or retried to get a result)
import com.google.gson.*;
import com.microsoft.playwright.*;
import com.microsoft.playwright.options.WaitUntilState;
import java.net.URI;
import java.net.http.*;
import java.security.SecureRandom;
import java.time.Instant;
import java.util.*;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");
  static final Gson GSON = new GsonBuilder().setPrettyPrinting().disableHtmlEscaping().serializeNulls().create();
  static final HttpClient HTTP = HttpClient.newHttpClient();
  // One sticky residential exit for the whole run, shared by every setup: a difference between rows is the browser
  // and how it was driven, not the IP.
  static final String PROXY = String.format("cdpfleet-resi-country-us-session-c81%06x-lifetime-30", new SecureRandom().nextInt(1 << 24));
  static final int REPEATS = 3;

  record Setup(String key, String name, String engine, String type, boolean headless, Object humanize) {}

  // Camoufox's humanize option: false = the cursor jumps straight to the target, true (the default) = a curved path of
  // up to 1.5 s, a number = a path of up to that many seconds. Chrome is the reference: plain Playwright clicks.
  static final List<Setup> SETUPS = List.of(
      new Setup("cf_off", "Camoufox, humanize: false", "camoufox", "firefox", true, false),
      new Setup("cf_on", "Camoufox, humanize: true (default)", "camoufox", "firefox", true, true),
      new Setup("cf_slow", "Camoufox, humanize: 3", "camoufox", "firefox", true, 3),
      new Setup("chrome", "Google Chrome, headful", "chrome", "chromium", false, null));
  // The same actions in every cell; only the typing speed differs between the two form runs of a session.
  static final LinkedHashMap<String, Double> TYPING = new LinkedHashMap<>(); // ms between keys
  static {
    TYPING.put("fast", 0.0);
    TYPING.put("paced", 120.0);
  }
  static final String EMAIL = "lab81@example.com";
  static final String PASSWORD = "lab-81!pass"; // the demo form wants 8+ characters with a special character

  // deviceandbrowserinfo.com's behavioural bot test: a demo login form that records clicks, keystrokes and timing
  // and, on submit, posts them with a fingerprint to its own endpoint, which answers human or bot with its flags.
  static final String FORM = "https://deviceandbrowserinfo.com/are_you_a_bot_interactions";
  // Google's public reCAPTCHA v3 demo prints its server's verify response (with the score); its "Try again" link reloads it.
  static final String RECAPTCHA = "https://recaptcha-demo.appspot.com/recaptcha-v3-request-scores.php";

  // The same expressions run in every language.
  // Count mousemove events on the page, so the table shows what humanize changed.
  static final String COUNT_MOVES = "(() => { window.__lab81 = 0; document.addEventListener('mousemove', () => { window.__lab81++; }, true); return true; })()";
  // The verdict the page shows after submit: isBot and every detail flag that is true.
  static final String VERDICT = """
      (() => {
        const e = document.getElementById('jsonResult');
        const t = e ? e.innerText : '';
        if (t.length < 10) return null;
        const r = JSON.parse(t);
        const d = r.details || {};
        return { is_bot: r.isBot === true, flags: Object.keys(d).filter((k) => d[k] === true), moves: window.__lab81 };
      })()""";
  // What the page itself measured about the interaction: the "interactions" part of the body it posted, which is
  // embedded as a JSON string and passed to this function.
  static final String MEASURED = """
      (body) => {
        const i = JSON.parse(body).interactions || {};
        const s = i.emailTimingStats || {};
        return {
          time_to_fill_ms: Math.round(i.timeToFill),
          exact_center_clicks: [i.hasClickedEmailFieldExactCenter, i.hasClickedPasswordFieldExactCenter, i.hasClickedSubmitExactCenter].filter(Boolean).length,
          untrusted_event: i.hasUntrustedEvent === true,
          cdp_mouse_leak: i.cdpMouseLeak === true,
          key_gap_median_ms: typeof s.medianTimeBetweenKeys === 'number' ? Math.round(s.medianTimeBetweenKeys) : null,
        };
      }""";
  static final String SCORE = "(() => { if (window.__lab81rc) return null; const m = /\"score\"\\s*:\\s*([0-9.]+)/.exec(document.body ? document.body.innerText : ''); return m ? Number(m[1]) : null; })()";

  static Playwright playwright;

  static Browser launch(Setup s) throws Exception {
    JsonObject body = new JsonObject();
    body.addProperty("proxy", PROXY);
    body.addProperty("record", false);
    body.addProperty("headless", s.headless());
    if (s.humanize() instanceof Boolean b) body.addProperty("humanize", b);
    else if (s.humanize() instanceof Number n) body.addProperty("humanize", n);
    for (int attempt = 1; ; attempt++) {
      HttpResponse<String> res = HTTP.send(HttpRequest.newBuilder(URI.create("https://starter.cdpfleet.com/" + s.engine() + "/session"))
          .header("x-api-key", KEY).header("content-type", "application/json")
          .POST(HttpRequest.BodyPublishers.ofString(body.toString())).build(), HttpResponse.BodyHandlers.ofString());
      if (res.statusCode() == 200) {
        String wsUrl = JsonParser.parseString(res.body()).getAsJsonObject().get("wsUrl").getAsString();
        BrowserType type = s.type().equals("firefox") ? playwright.firefox() : playwright.chromium();
        return type.connect(wsUrl, new BrowserType.ConnectOptions().setHeaders(Map.of("x-api-key", KEY)));
      }
      if ((res.statusCode() != 429 && res.statusCode() != 503) || attempt == 5) throw new RuntimeException("launch " + res.statusCode() + " " + res.body());
      Thread.sleep(Long.parseLong(res.headers().firstValue("retry-after").orElse("5")) * 1000);
    }
  }

  static Object safe(Page tab, String expr) {
    try { return tab.evaluate(expr); } catch (PlaywrightException err) { return null; }
  }

  // A load that fails on the network (proxy timeout) is tried once more; the page's answer is never retried.
  static boolean open(Page tab, String url) {
    for (int attempt = 1; attempt <= 2; attempt++) {
      try {
        tab.navigate(url, new Page.NavigateOptions().setWaitUntil(WaitUntilState.DOMCONTENTLOADED).setTimeout(30000));
        return true;
      } catch (PlaywrightException err) { /* network failure */ }
    }
    return false;
  }

  static JsonObject emptyRun(JsonObject base, String typing) {
    JsonObject out = base.deepCopy();
    out.addProperty("typing", typing);
    out.addProperty("verdict", "error");
    out.add("flags", new JsonArray());
    out.add("moves", JsonNull.INSTANCE);
    out.add("action_seconds", JsonNull.INSTANCE);
    out.add("measured", JsonNull.INSTANCE);
    return out;
  }

  // One form run: click the email field, type, click the password field, type, click Login; read the verdict.
  static JsonObject form(Browser browser, JsonObject base, String typing) throws Exception {
    JsonObject out = emptyRun(base, typing);
    BrowserContext context = browser.newContext(); // a fresh context: every run is a first visit
    try {
      Page tab = context.newPage();
      String[] posted = {null};
      tab.onRequest(req -> { if (req.url().contains("/fingerprint_bot_test")) posted[0] = req.postData(); });
      if (!open(tab, FORM)) return out;
      Thread.sleep(1000);
      tab.evaluate(COUNT_MOVES);
      long t0 = System.currentTimeMillis();
      double delay = TYPING.get(typing);
      tab.click("#email");
      tab.keyboard().type(EMAIL, new Keyboard.TypeOptions().setDelay(delay));
      tab.click("#password");
      tab.keyboard().type(PASSWORD, new Keyboard.TypeOptions().setDelay(delay));
      tab.click("#loginForm button[type=submit]");
      out.addProperty("action_seconds", Math.round((System.currentTimeMillis() - t0) / 100.0) / 10.0);
      Object v = null;
      for (int i = 0; i < 60 && v == null; i++) { Thread.sleep(500); v = safe(tab, VERDICT); }
      if (v == null) { out.addProperty("verdict", "no verdict"); return out; }
      JsonObject verdict = GSON.toJsonTree(v).getAsJsonObject();
      out.addProperty("verdict", verdict.get("is_bot").getAsBoolean() ? "bot" : "human");
      out.add("flags", verdict.get("flags"));
      out.addProperty("moves", verdict.get("moves").getAsInt());
      if (posted[0] != null) {
        JsonObject m = GSON.toJsonTree(tab.evaluate("(" + MEASURED + ")(" + new Gson().toJson(posted[0]) + ")")).getAsJsonObject();
        JsonObject measured = new JsonObject();
        measured.addProperty("time_to_fill_ms", m.get("time_to_fill_ms").getAsInt());
        measured.addProperty("exact_center_clicks", m.get("exact_center_clicks").getAsInt());
        measured.add("untrusted_event", m.get("untrusted_event"));
        measured.add("cdp_mouse_leak", m.get("cdp_mouse_leak"));
        JsonElement gap = m.get("key_gap_median_ms");
        if (gap == null || gap.isJsonNull()) measured.add("key_gap_median_ms", JsonNull.INSTANCE);
        else measured.addProperty("key_gap_median_ms", gap.getAsInt());
        out.add("measured", measured);
      }
      return out;
    } catch (RuntimeException err) { // a PlaywrightException, or a verdict without a field
      return out;
    } finally {
      try { context.close(); } catch (PlaywrightException err) { }
    }
  }

  static void putScore(JsonObject out, String name, Object score) {
    if (score == null) out.add(name, JsonNull.INSTANCE); else out.addProperty(name, ((Number) score).doubleValue());
  }

  // Google's v3 demo: wait for the score it asks for on load, move the cursor and click "Try again", read the new score.
  static JsonObject recaptcha(Browser browser, JsonObject base) throws Exception {
    JsonObject out = base.deepCopy();
    Object before = null, after = null;
    BrowserContext context = browser.newContext();
    try {
      Page tab = context.newPage();
      if (open(tab, RECAPTCHA)) {
        for (int i = 0; i < 40 && before == null; i++) { Thread.sleep(500); before = safe(tab, SCORE); }
        tab.evaluate("window.__lab81rc = 1"); // marks this document, so the score is read from the reloaded one
        tab.mouse().move(300, 300);
        tab.mouse().move(600, 200);
        tab.click("a[href=\"/recaptcha-v3-request-scores.php\"]");
        for (int i = 0; i < 40 && after == null; i++) { Thread.sleep(500); after = safe(tab, SCORE); }
      }
    } catch (PlaywrightException err) {
      // keep what was read
    } finally {
      try { context.close(); } catch (PlaywrightException err) { }
    }
    putScore(out, "before", before);
    putScore(out, "after", after);
    return out;
  }

  public static void main(String[] args) throws Exception {
    playwright = Playwright.create();
    JsonArray runs = new JsonArray();
    JsonArray scores = new JsonArray();
    // Serial: one session at a time; the repeats are interleaved so a slow minute doesn't land on one setup.
    for (int repeat = 1; repeat <= REPEATS; repeat++) {
      for (Setup s : SETUPS) {
        JsonObject base = new JsonObject();
        base.addProperty("setup", s.name());
        base.addProperty("key", s.key());
        base.addProperty("repeat", repeat);
        Browser browser;
        try {
          browser = launch(s);
        } catch (Exception err) {
          for (String typing : TYPING.keySet()) runs.add(emptyRun(base, typing));
          JsonObject sc = base.deepCopy();
          sc.add("before", JsonNull.INSTANCE);
          sc.add("after", JsonNull.INSTANCE);
          scores.add(sc);
          continue;
        }
        // A session the server ends leaves the remaining cells as errors.
        for (String typing : TYPING.keySet()) {
          try { runs.add(form(browser, base, typing)); } catch (PlaywrightException err) { runs.add(emptyRun(base, typing)); }
        }
        try {
          scores.add(recaptcha(browser, base));
        } catch (PlaywrightException err) {
          JsonObject sc = base.deepCopy();
          sc.add("before", JsonNull.INSTANCE);
          sc.add("after", JsonNull.INSTANCE);
          scores.add(sc);
        }
        try { browser.close(); } catch (PlaywrightException err) { }
      }
    }
    playwright.close();
    JsonObject result = new JsonObject();
    result.addProperty("measured_at", Instant.now().toString().substring(0, 10));
    result.add("runs", runs);
    result.add("recaptcha", scores);
    System.out.println(GSON.toJson(result));
  }
}
