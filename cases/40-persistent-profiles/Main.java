// Maven: com.microsoft.playwright:playwright:1.60.0, com.google.code.gson:gson:2.11.0
// Run with PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1. env: CDPFLEET_API_KEY, PROXY_URL
import com.google.gson.*;
import com.microsoft.playwright.*;
import java.net.URI;
import java.net.http.*;
import java.nio.ByteBuffer;
import java.security.SecureRandom;
import java.util.*;
import java.util.concurrent.CompletableFuture;
import java.util.regex.Pattern;
import javax.crypto.Mac;
import javax.crypto.spec.SecretKeySpec;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");
  static final String NAME = "case40-java-" + HexFormat.of().formatHex(SecureRandom.getSeed(4)); // one profile per run
  static final String PRACTICE = "https://authenticationtest.com";
  static final String SITE = "https://httpbin.org/html";
  // The practice site publishes its test account and TOTP secret on the challenge page.
  static final String EMAIL = "totp@authenticationtest.com", PASSWORD = "pa$$w0rd", SECRET = "I65VU7K5ZQL7WB4E";
  static final HttpClient HTTP = HttpClient.newHttpClient();
  static final Gson GSON = new Gson();

  // Writes every kind of storage on httpbin.org, and reads it back.
  static final String WRITE_JS = """
async () => {
  document.cookie = 'remember_me=alice; max-age=2592000; path=/';  // what "Remember me" sets
  document.cookie = 'session_only=yes; path=/';                    // no expiry: a session cookie
  localStorage.setItem('theme', 'dark');
  sessionStorage.setItem('draft', 'unsaved');
  await new Promise((resolve, reject) => {
    const open = indexedDB.open('app', 1);
    open.onupgradeneeded = () => open.result.createObjectStore('auth');
    open.onsuccess = () => {
      const tx = open.result.transaction('auth', 'readwrite');
      tx.objectStore('auth').put({ email: 'alice@example.com', token: 'demo-token' }, 'user');
      tx.oncomplete = () => { open.result.close(); resolve(); };
      tx.onerror = () => reject(tx.error);
    };
  });
}
      """;
  static final String READ_JS = """
async () => {
  const cookies = Object.fromEntries(document.cookie.split('; ').filter(Boolean).map((c) => c.split('=')));
  const idb = await new Promise((resolve) => {
    const open = indexedDB.open('app', 1);
    open.onupgradeneeded = () => open.result.createObjectStore('auth');
    open.onsuccess = () => {
      const db = open.result;
      if (!db.objectStoreNames.contains('auth')) { db.close(); resolve(null); return; }
      const get = db.transaction('auth').objectStore('auth').get('user');
      get.onsuccess = () => { db.close(); resolve(get.result ?? null); };
      get.onerror = () => { db.close(); resolve(null); };
    };
    open.onerror = () => resolve(null);
  });
  return {
    persistent_cookie: cookies.remember_me === 'alice',
    session_cookie: cookies.session_only === 'yes',
    local_storage: localStorage.getItem('theme') === 'dark',
    session_storage: sessionStorage.getItem('draft') === 'unsaved',
    indexeddb: idb?.email === 'alice@example.com',
  };
}
      """;

  // RFC 6238 TOTP (SHA-1, 6 digits, 30 s steps), as in the TOTP login case.
  static String totp(String secret, double unixSeconds) throws Exception {
    String alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567";
    ByteBuffer key = ByteBuffer.allocate(secret.length() * 5 / 8);
    int buffer = 0, bits = 0;
    for (char c : secret.toCharArray()) {
      buffer = (buffer << 5) | alphabet.indexOf(c);
      bits += 5;
      if (bits >= 8) { key.put((byte) (buffer >> (bits - 8))); bits -= 8; }
    }
    Mac mac = Mac.getInstance("HmacSHA1");
    mac.init(new SecretKeySpec(key.array(), "HmacSHA1"));
    byte[] h = mac.doFinal(ByteBuffer.allocate(8).putLong((long) Math.floor(unixSeconds / 30)).array());
    int o = h[h.length - 1] & 15;
    return String.format("%06d", (ByteBuffer.wrap(h, o, 4).getInt() & 0x7fffffff) % 1000000);
  }

  static double now() { return System.currentTimeMillis() / 1000.0; }

  static HttpResponse<String> api(String method, String path) throws Exception {
    return HTTP.send(HttpRequest.newBuilder(URI.create("https://cdpfleet.com" + path)).header("x-api-key", KEY)
        .method(method, HttpRequest.BodyPublishers.noBody()).build(), HttpResponse.BodyHandlers.ofString());
  }

  static JsonObject profileInfo() throws Exception {
    for (JsonElement p : JsonParser.parseString(api("GET", "/v1/me/profiles").body()).getAsJsonObject().getAsJsonArray("profiles")) {
      if (p.getAsJsonObject().get("name").getAsString().equals(NAME)) return p.getAsJsonObject();
    }
    return null;
  }

  // The node saves the profile after the session's browser has exited: wait for the new version.
  static JsonObject waitForVersion(int version) throws Exception {
    for (int i = 0; i < 60; i++) {
      JsonObject p = profileInfo();
      if (p != null && p.get("version").getAsInt() >= version && !p.get("in_use").getAsBoolean()) return p;
      Thread.sleep(2000);
    }
    throw new RuntimeException("profile " + NAME + " never reached version " + version);
  }

  record Started(int status, JsonObject body, long t) {}

  // POST the launch with the profile (thread-safe: no Playwright here).
  static Started start(JsonElement profile) {
    try {
      JsonObject req = new JsonObject();
      req.addProperty("proxy", System.getenv("PROXY_URL"));
      req.addProperty("headless", true);
      req.add("profile", profile);
      long t = System.currentTimeMillis();
      HttpResponse<String> res = HTTP.send(HttpRequest.newBuilder(URI.create("https://starter.cdpfleet.com/chromium/session"))
          .header("x-api-key", KEY).header("content-type", "application/json")
          .POST(HttpRequest.BodyPublishers.ofString(req.toString())).build(), HttpResponse.BodyHandlers.ofString());
      return new Started(res.statusCode(), JsonParser.parseString(res.body()).getAsJsonObject(), t);
    } catch (Exception e) {
      throw new RuntimeException(e);
    }
  }

  record Attached(int status, String error, String sessionId, Browser browser, BrowserContext context, Page page, long launchMs) {}

  // Connect to a launched session and open a page in the profile's context.
  static Attached attach(Playwright playwright, Started s) {
    if (s.status() != 200) return new Attached(s.status(), s.body().get("error").getAsString(), null, null, null, null, 0);
    Browser browser = playwright.chromium().connect(s.body().get("wsUrl").getAsString(),
        new BrowserType.ConnectOptions().setHeaders(Map.of("x-api-key", KEY)));
    BrowserContext context = browser.contexts().get(0); // the profile — a newContext() would not be saved
    Page page = context.pages().isEmpty() ? context.newPage() : context.pages().get(0);
    return new Attached(s.status(), null, s.body().get("sessionId").getAsString(), browser, context, page, System.currentTimeMillis() - s.t());
  }

  // Proxies drop a tunnel now and then: up to two retries.
  static void go(Page page, String url) throws InterruptedException {
    for (int attempt = 1; ; attempt++) {
      try {
        page.navigate(url, new Page.NavigateOptions().setTimeout(60000));
        return;
      } catch (PlaywrightException e) {
        if (attempt == 3) throw e;
        Thread.sleep(2000);
      }
    }
  }

  static JsonObject storage(Page page) throws InterruptedException {
    go(page, SITE);
    return GSON.toJsonTree(page.evaluate(READ_JS)).getAsJsonObject();
  }

  static boolean signedIntoPractice(Page page) throws InterruptedException {
    go(page, PRACTICE + "/");
    return page.locator("text=Sign Out").count() > 0;
  }

  static JsonObject statusError(Attached a) {
    JsonObject o = new JsonObject();
    o.addProperty("status", a.status());
    if (a.error() == null) o.add("error", JsonNull.INSTANCE); else o.addProperty("error", a.error());
    return o;
  }

  public static void main(String[] args) throws Exception {
    JsonObject out = new JsonObject();
    out.addProperty("profile_name_prefix", "case40-");
    JsonObject sessions = new JsonObject(), first = new JsonObject(), saved = new JsonObject(), second = new JsonObject(), after = new JsonObject();
    JsonArray readonly = new JsonArray();
    out.add("sessions", sessions);
    out.add("first", first);
    out.add("saved", saved);
    out.add("second", second);
    out.add("new_context", new JsonObject());
    out.add("second_writer", new JsonObject());
    out.add("readonly", readonly);
    out.add("after", after);
    try (Playwright playwright = Playwright.create()) {
      try {
        // Session A: a new profile. Log in (TOTP) and write every kind of storage.
        Attached a = attach(playwright, start(new JsonPrimitive(NAME)));
        if (a.browser() == null) throw new RuntimeException("launch A " + a.status() + " " + a.error());
        sessions.addProperty("a", a.sessionId());
        first.addProperty("launch_ms", a.launchMs());
        try {
          go(a.page(), PRACTICE + "/totpChallenge/");
          if (30 - now() % 30 < 5) Thread.sleep(5200); // fresh TOTP window
          a.page().fill("#email", EMAIL);
          a.page().fill("#password", PASSWORD);
          a.page().fill("#totpmfa", totp(SECRET, now()));
          a.page().click("input[type=submit]");
          a.page().waitForURL(Pattern.compile("/login(Success|Fail)/"), new Page.WaitForURLOptions().setTimeout(60000));
          first.addProperty("practice_login", URI.create(a.page().url()).getPath().equals("/loginSuccess/"));
          JsonArray kinds = new JsonArray();
          for (var c : a.context().cookies(PRACTICE)) kinds.add(c.expires == null || c.expires == -1 ? "session" : "persistent");
          first.add("practice_cookie_expires", kinds);
          go(a.page(), SITE);
          a.page().evaluate(WRITE_JS);
          first.add("wrote", storage(a.page()));
        } finally {
          a.browser().close(); // ends the session; the node saves the profile once the browser has exited
        }
        JsonObject s1 = waitForVersion(1);
        saved.add("version", s1.get("version"));
        saved.add("bytes", s1.get("bytes"));

        // Session B: the same profile, restored.
        Attached b = attach(playwright, start(new JsonPrimitive(NAME)));
        if (b.browser() == null) throw new RuntimeException("launch B " + b.status() + " " + b.error());
        sessions.addProperty("b", b.sessionId());
        second.addProperty("launch_ms", b.launchMs());
        try {
          second.add("restored", storage(b.page()));
          second.addProperty("practice_signed_in", signedIntoPractice(b.page()));

          // The trap: a new context in the same session starts empty and is never saved.
          BrowserContext scratch = b.browser().newContext();
          out.add("new_context", storage(scratch.newPage()));
          scratch.close();

          // While B holds the profile: a second normal launch is refused, read-only ones work.
          Attached w = attach(playwright, start(new JsonPrimitive(NAME)));
          out.add("second_writer", statusError(w));
          if (w.browser() != null) w.browser().close();
          JsonObject ro = new JsonObject();
          ro.addProperty("id", NAME);
          ro.addProperty("mode", "readonly");
          // Launch the readers in parallel (HTTP only); Playwright is driven from this thread.
          List<CompletableFuture<Started>> launches = new ArrayList<>();
          for (int i = 0; i < 3; i++) launches.add(CompletableFuture.supplyAsync(() -> start(ro)));
          for (CompletableFuture<Started> f : launches) {
            Attached r = attach(playwright, f.join());
            if (r.browser() == null) { readonly.add(statusError(r)); continue; }
            try {
              JsonObject s = storage(r.page());
              JsonObject entry = new JsonObject();
              entry.addProperty("status", r.status());
              entry.add("persistent_cookie", s.get("persistent_cookie"));
              entry.add("local_storage", s.get("local_storage"));
              entry.add("indexeddb", s.get("indexeddb"));
              readonly.add(entry);
            } finally {
              r.browser().close();
            }
          }
          b.page().evaluate("() => document.title"); // keep B active (idle sessions end after 60 s on shared threads)
          JsonObject info = profileInfo();
          after.add("version_after_readonly", info == null ? JsonNull.INSTANCE : info.get("version"));
        } finally {
          b.browser().close();
        }
        after.add("version_after_b", waitForVersion(2).get("version"));
      } finally {
        // Delete the profile once no session holds it.
        for (int i = 0; i < 60; i++) {
          JsonObject info = profileInfo();
          if (info == null || !info.get("in_use").getAsBoolean()) break;
          Thread.sleep(2000);
        }
        int status = api("DELETE", "/v1/me/profiles/" + NAME).statusCode();
        after.addProperty("deleted", status == 200 && profileInfo() == null);
      }
    }
    System.out.println(new GsonBuilder().setPrettyPrinting().create().toJson(out));
  }
}
