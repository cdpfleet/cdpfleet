// Maven: com.microsoft.playwright:playwright:1.60.0, com.google.code.gson:gson:2.11.0
// Run with PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1. env: CDPFLEET_API_KEY, PROXY_URL
import com.google.gson.*;
import com.microsoft.playwright.*;
import java.net.URI;
import java.net.http.*;
import java.nio.ByteBuffer;
import java.time.ZonedDateTime;
import java.time.format.DateTimeFormatter;
import java.util.*;
import java.util.regex.Pattern;
import javax.crypto.Mac;
import javax.crypto.spec.SecretKeySpec;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");
  static final String SITE = "https://authenticationtest.com";
  // The practice site publishes its test account and TOTP secret on the challenge page.
  static final String EMAIL = "totp@authenticationtest.com", PASSWORD = "pa$$w0rd", SECRET = "I65VU7K5ZQL7WB4E";

  // RFC 6238 TOTP (SHA-1, 6 digits, 30 s steps) from the base32 secret — what an authenticator app does.
  static String totp(String secret, double unixSeconds) throws Exception {
    String alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567";
    ByteBuffer key = ByteBuffer.allocate(secret.length() * 5 / 8);
    int buffer = 0, bits = 0;
    for (char c : secret.replace("=", "").toCharArray()) {
      buffer = (buffer << 5) | alphabet.indexOf(c);
      bits += 5;
      if (bits >= 8) { key.put((byte) (buffer >> (bits - 8))); bits -= 8; }
    }
    Mac mac = Mac.getInstance("HmacSHA1");
    mac.init(new SecretKeySpec(key.array(), "HmacSHA1"));
    byte[] h = mac.doFinal(ByteBuffer.allocate(8).putLong((long) Math.floor(unixSeconds / 30)).array());
    int o = h[h.length - 1] & 15;
    int value = (ByteBuffer.wrap(h, o, 4).getInt() & 0x7fffffff) % 1000000;
    return String.format("%06d", value);
  }

  static double now() { return System.currentTimeMillis() / 1000.0; }
  static double round1(double v) { return Math.round(v * 10) / 10.0; }

  // A code typed in the last seconds of its window can expire before the server checks it:
  // wait for the next window when fewer than 5 s are left.
  static JsonObject freshWindow() throws InterruptedException {
    double left = 30 - now() % 30;
    if (left < 5) Thread.sleep((long) (left * 1000) + 200);
    JsonObject r = new JsonObject();
    r.addProperty("seconds_left_in_window", round1(left));
    r.addProperty("waited_s", left < 5 ? round1(left) : 0);
    return r;
  }

  record Session(String id, Browser browser) {}

  static Session launch(Playwright playwright) throws Exception {
    String body = "{\"proxy\": " + new Gson().toJson(System.getenv("PROXY_URL")) + ", \"headless\": true}";
    HttpResponse<String> res = HttpClient.newHttpClient().send(HttpRequest.newBuilder(URI.create("https://starter.cdpfleet.com/chromium/session"))
        .header("x-api-key", KEY).header("content-type", "application/json")
        .POST(HttpRequest.BodyPublishers.ofString(body)).build(), HttpResponse.BodyHandlers.ofString());
    if (res.statusCode() / 100 != 2) throw new RuntimeException("launch: " + res.statusCode() + " " + res.body());
    JsonObject session = JsonParser.parseString(res.body()).getAsJsonObject();
    Browser browser = playwright.chromium().connect(session.get("wsUrl").getAsString(),
        new BrowserType.ConnectOptions().setHeaders(Map.of("x-api-key", KEY)));
    return new Session(session.get("sessionId").getAsString(), browser);
  }

  record Attempt(BrowserContext context, JsonObject result) {}

  // One login attempt in a fresh context (no cookies from earlier attempts). The code is made
  // after the form has loaded, right before typing it: offset null types a wrong code, otherwise
  // the code for now + offset seconds.
  // The challenge page or the post-submit navigation occasionally hangs through a rotating proxy:
  // one more try in a fresh context, with the code made again after the reload.
  static Attempt attempt(Browser browser, String label, Integer offset) throws Exception {
    try {
      return attemptOnce(browser, label, offset);
    } catch (com.microsoft.playwright.TimeoutError e) {
      for (BrowserContext c : new java.util.ArrayList<>(browser.contexts())) {
        try { c.close(); } catch (Exception ignored) { }
      }
      return attemptOnce(browser, label, offset);
    }
  }

  static Attempt attemptOnce(Browser browser, String label, Integer offset) throws Exception {
    BrowserContext context = browser.newContext();
    Page page = context.newPage();
    page.navigate(SITE + "/totpChallenge/", new Page.NavigateOptions().setTimeout(60000));
    JsonObject window = freshWindow();
    String code = offset == null ? "000000" : totp(SECRET, now() + offset);
    long t = System.currentTimeMillis();
    page.fill("#email", EMAIL);
    page.fill("#password", PASSWORD);
    page.fill("#totpmfa", code);
    page.click("input[type=submit]");
    page.waitForURL(Pattern.compile("/login(Success|Fail)/"), new Page.WaitForURLOptions().setTimeout(60000));
    String landed = URI.create(page.url()).getPath();
    JsonObject r = new JsonObject();
    r.addProperty("attempt", label);
    if (offset == null) r.add("offset_s", JsonNull.INSTANCE); else r.addProperty("offset_s", offset);
    r.add("seconds_left_in_window", window.get("seconds_left_in_window"));
    r.add("waited_s", window.get("waited_s"));
    r.addProperty("landed", landed);
    r.addProperty("accepted", landed.equals("/loginSuccess/"));
    r.addProperty("ms", System.currentTimeMillis() - t);
    return new Attempt(context, r);
  }

  public static void main(String[] args) throws Exception {
    JsonObject out = new JsonObject();
    out.addProperty("site", SITE + "/totpChallenge/");
    JsonObject sessionIds = new JsonObject();
    out.add("session_ids", sessionIds);
    JsonObject clocks = new JsonObject();
    JsonArray attempts = new JsonArray();
    String state;
    try (Playwright playwright = Playwright.create()) {
      Session first = launch(playwright);
      sessionIds.addProperty("first", first.id());
      Browser browser = first.browser();
      try {
        // What matters is your clock against the server's. The site's Date header (from its CDN
        // edge, 1 s resolution) should fall between our clock just before and just after the
        // request. The remote browser's clock is recorded for comparison only.
        Page page = browser.newPage();
        double t0 = now();
        Response resp = page.navigate(SITE + "/totpChallenge/", new Page.NavigateOptions().setTimeout(60000));
        double t1 = now();
        long server = ZonedDateTime.parse(resp.headers().get("date"), DateTimeFormatter.RFC_1123_DATE_TIME).toEpochSecond();
        clocks.addProperty("server_date_within_request", server >= Math.floor(t0) - 1 && server <= t1 + 1);
        clocks.addProperty("request_s", round1(t1 - t0));
        clocks.addProperty("browser_minus_local_s", round1(((Number) page.evaluate("Date.now()")).doubleValue() / 1000 - now()));
        page.close();
        out.add("clocks", clocks);
        out.add("attempts", attempts);

        Object[][] tries = {
          { "wrong code", null },
          { "code from 2 windows ago (-60 s)", -60 },
          { "code from the previous window (-30 s)", -30 },
          { "code from the next window (+30 s)", 30 },
        };
        for (Object[] tr : tries) {
          Attempt a = attempt(browser, (String) tr[0], (Integer) tr[1]);
          attempts.add(a.result());
          a.context().close();
        }
        Attempt current = attempt(browser, "current code", 0);
        attempts.add(current.result());
        state = current.context().storageState();
        JsonArray cookies = JsonParser.parseString(state).getAsJsonObject().getAsJsonArray("cookies");
        int session = 0;
        for (JsonElement c : cookies) if (c.getAsJsonObject().get("expires").getAsDouble() == -1) session++;
        JsonObject saved = new JsonObject();
        saved.addProperty("cookies", cookies.size());
        saved.addProperty("session_cookies", session);
        out.add("storage_state", saved);
      } finally {
        browser.close();
      }

      // A new session (another browser): without the saved state, then with it.
      Session secondLaunch = launch(playwright);
      sessionIds.addProperty("second", secondLaunch.id());
      Browser second = secondLaunch.browser();
      try {
        JsonObject secondSession = new JsonObject();
        Map<String, Browser.NewContextOptions> setups = new LinkedHashMap<>();
        setups.put("without_state", new Browser.NewContextOptions());
        setups.put("with_state", new Browser.NewContextOptions().setStorageState(state));
        for (Map.Entry<String, Browser.NewContextOptions> setup : setups.entrySet()) {
          BrowserContext context = second.newContext(setup.getValue());
          Page page = context.newPage();
          page.navigate(SITE + "/", new Page.NavigateOptions().setTimeout(60000));
          JsonObject r = new JsonObject();
          r.addProperty("signed_in", page.locator("text=Sign Out").count() > 0);
          secondSession.add(setup.getKey(), r);
          context.close();
        }
        out.add("second_session", secondSession);
      } finally {
        second.close();
      }
    }
    System.out.println(new GsonBuilder().serializeNulls().setPrettyPrinting().disableHtmlEscaping().create().toJson(out));
  }
}
