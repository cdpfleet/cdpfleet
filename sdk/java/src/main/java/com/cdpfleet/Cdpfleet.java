// cdpfleet SDK for Java — launch a remote browser and connect your own Playwright to it.
// Spec shared by every language: ../../../../../../SPEC.md
package com.cdpfleet;

import com.google.gson.Gson;
import com.google.gson.GsonBuilder;
import com.google.gson.JsonElement;
import com.google.gson.JsonNull;
import com.google.gson.JsonObject;
import com.google.gson.JsonParser;
import com.google.gson.Strictness;
import com.google.gson.stream.JsonReader;
import com.google.gson.stream.JsonToken;

import java.io.IOException;
import java.io.StringReader;
import java.net.URI;
import java.net.URLEncoder;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.nio.charset.StandardCharsets;
import java.time.Duration;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import java.util.Set;
import java.util.concurrent.ThreadLocalRandom;

/**
 * The cdpfleet client: launches remote browsers ({@link #launch}) and reads the account API.
 *
 * <pre>{@code
 * Cdpfleet fleet = new Cdpfleet(); // reads CDPFLEET_API_KEY
 * try (Session s = fleet.launch("chrome", Map.of("headless", true))) {
 *   Browser browser = s.connect(playwright.chromium());
 * }
 * }</pre>
 */
public class Cdpfleet {
  public static final String VERSION = "0.1.0";
  static final String UA = "cdpfleet-sdk-java/" + VERSION;
  static final Gson GSON = new GsonBuilder().serializeNulls().disableHtmlEscaping().create();
  private static final Set<String> QUOTA = Set.of("daily_quota_exhausted", "hourly_quota_exhausted");

  /** Constructor options; every one is optional. */
  public static class Options {
    String apiKey;
    String starterUrl = "https://starter.cdpfleet.com";
    String apiUrl = "https://cdpfleet.com";
    double maxWait = 120;

    /** API key; defaults to env {@code CDPFLEET_API_KEY}. */
    public Options setApiKey(String apiKey) { this.apiKey = apiKey; return this; }
    /** Launch endpoint; defaults to {@code https://starter.cdpfleet.com}. */
    public Options setStarterUrl(String starterUrl) { this.starterUrl = starterUrl; return this; }
    /** Account API; defaults to {@code https://cdpfleet.com}. */
    public Options setApiUrl(String apiUrl) { this.apiUrl = apiUrl; return this; }
    /** How long {@link Cdpfleet#launch} keeps retrying, in seconds; defaults to 120. */
    public Options setMaxWait(double seconds) { this.maxWait = seconds; return this; }
  }

  final String apiKey;
  final String starterUrl;
  final String apiUrl;
  final double maxWait;
  final HttpClient http;

  /** A client with the API key from env {@code CDPFLEET_API_KEY}. */
  public Cdpfleet() { this(new Options()); }

  /** A client with the given API key. */
  public Cdpfleet(String apiKey) { this(new Options().setApiKey(apiKey)); }

  public Cdpfleet(Options options) {
    Options o = options == null ? new Options() : options;
    String key = o.apiKey != null ? o.apiKey : System.getenv("CDPFLEET_API_KEY");
    if (key == null || key.isEmpty()) throw new IllegalArgumentException("cdpfleet: no API key (pass apiKey or set CDPFLEET_API_KEY)");
    this.apiKey = key;
    this.starterUrl = o.starterUrl.replaceAll("/$", "");
    this.apiUrl = o.apiUrl.replaceAll("/$", "");
    this.maxWait = o.maxWait;
    this.http = HttpClient.newBuilder()
        .version(HttpClient.Version.HTTP_1_1)
        .followRedirects(HttpClient.Redirect.NORMAL)
        .connectTimeout(Duration.ofSeconds(30))
        .build();
  }

  public String apiKey() { return apiKey; }

  HttpRequest.Builder request(String url, Duration timeout) {
    return HttpRequest.newBuilder(URI.create(url)).timeout(timeout).header("x-api-key", apiKey).header("user-agent", UA);
  }

  static HttpRequest.BodyPublisher json(Object body) {
    return HttpRequest.BodyPublishers.ofString(GSON.toJson(body), StandardCharsets.UTF_8);
  }

  static JsonElement readJson(String text) {
    if (text == null || text.isEmpty()) return JsonNull.INSTANCE;
    try {
      JsonReader reader = new JsonReader(new StringReader(text));
      reader.setStrictness(Strictness.STRICT);
      JsonElement json = JsonParser.parseReader(reader);
      if (reader.peek() != JsonToken.END_DOCUMENT) throw new IOException("trailing data");
      return json;
    } catch (Exception e) {
      JsonObject err = new JsonObject();
      err.addProperty("error", text.substring(0, Math.min(200, text.length())));
      return err;
    }
  }

  static JsonObject object(JsonElement json) {
    return json != null && json.isJsonObject() ? json.getAsJsonObject() : null;
  }

  static void sleep(double seconds) {
    try {
      Thread.sleep((long) (seconds * 1000));
    } catch (InterruptedException e) {
      Thread.currentThread().interrupt();
      throw CdpfleetException.of(0, "interrupted", "cdpfleet: interrupted");
    }
  }

  HttpResponse<String> send(HttpRequest req) throws IOException {
    try {
      return http.send(req, HttpResponse.BodyHandlers.ofString(StandardCharsets.UTF_8));
    } catch (InterruptedException e) {
      Thread.currentThread().interrupt();
      throw CdpfleetException.of(0, "interrupted", "cdpfleet: interrupted");
    }
  }

  /** {@link #launch(String, Map)} with no options. */
  public Session launch(String browser) { return launch(browser, Map.of()); }

  /**
   * Launches a browser; retries fleet backpressure (503) and rate limits (429) as the API asks.
   * {@code options} is sent as the JSON body untouched (e.g. {@code proxy}, {@code headless},
   * {@code cdp}, {@code keep_alive}, {@code record}, {@code proxy_rules}).
   */
  public Session launch(String browser, Map<String, Object> options) {
    Map<String, Object> opts = options == null ? Map.of() : options;
    long deadline = System.currentTimeMillis() + (long) (maxWait * 1000);
    HttpRequest req = request(starterUrl + "/" + browser + "/session", Duration.ofSeconds(60))
        .header("content-type", "application/json").POST(json(opts)).build();
    for (int attempt = 1; ; attempt++) {
      HttpResponse<String> res;
      JsonElement body;
      try {
        res = send(req);
        body = readJson(res.body());
      } catch (IOException e) {
        if (System.currentTimeMillis() > deadline) throw CdpfleetException.of(0, "network", "cdpfleet: launch failed: " + e);
        sleep(Math.min(Math.pow(2, attempt), 10));
        continue;
      }
      if (res.statusCode() == 200) return new Session(this, object(body), opts);
      String code = CdpfleetException.codeOf(body);
      boolean retryable = res.statusCode() == 503 || (res.statusCode() == 429 && !QUOTA.contains(code));
      if (!retryable || System.currentTimeMillis() > deadline) throw new CdpfleetException(res.statusCode(), body);
      double after = retryAfter(res.headers().firstValue("retry-after").orElse(null));
      if (after <= 0) after = Math.min(Math.pow(2, attempt - 1), 10);
      sleep(after + ThreadLocalRandom.current().nextDouble() * 0.5);
    }
  }

  private static double retryAfter(String value) {
    if (value == null) return 0;
    try {
      double v = Double.parseDouble(value.trim());
      return Double.isFinite(v) ? v : 0;
    } catch (NumberFormatException e) {
      return 0;
    }
  }

  JsonElement call(String method, String url, Object body) {
    HttpRequest.Builder b = request(url, Duration.ofSeconds(30));
    if (body != null) b.header("content-type", "application/json").method(method, json(body));
    else b.method(method, HttpRequest.BodyPublishers.noBody());
    HttpResponse<String> res;
    try {
      res = send(b.build());
    } catch (IOException e) {
      throw CdpfleetException.of(0, "network", "cdpfleet: " + method + " " + url + " failed: " + e);
    }
    JsonElement json = readJson(res.body());
    if (res.statusCode() < 200 || res.statusCode() > 299) throw new CdpfleetException(res.statusCode(), json);
    return json;
  }

  JsonObject get(String path) { return object(call("GET", apiUrl + path, null)); }

  /** {@code GET /v1/me}: the account and plan. */
  public JsonObject me() { return get("/v1/me"); }

  /** {@code GET /v1/me/usage}. */
  public JsonObject usage() { return get("/v1/me/usage"); }

  /** {@code GET /v1/me/threads}: the sessions running now. */
  public JsonObject threads() { return get("/v1/me/threads"); }

  /** {@code GET /v1/me/sessions}: session history. */
  public JsonObject sessions() { return sessions(null, null); }

  /** {@code GET /v1/me/sessions?days=&limit=}; null leaves a parameter out. */
  public JsonObject sessions(Integer days, Integer limit) {
    List<String> q = new ArrayList<>();
    if (days != null) q.add("days=" + days);
    if (limit != null) q.add("limit=" + limit);
    return get("/v1/me/sessions" + (q.isEmpty() ? "" : "?" + String.join("&", q)));
  }

  /** {@code GET /v1/me/recordings}. */
  public JsonObject recordings() { return get("/v1/me/recordings"); }

  /** A signed download URL for a recording. */
  public String recordingUrl(String id) {
    JsonObject res = get("/v1/me/recordings/" + enc(id) + "/url");
    JsonElement url = res == null ? null : res.get("url");
    return url == null || url.isJsonNull() ? null : url.getAsString();
  }

  /** {@code DELETE /v1/me/recordings/{id}}. */
  public JsonObject deleteRecording(String id) { return object(call("DELETE", apiUrl + "/v1/me/recordings/" + enc(id), null)); }

  /** {@code GET /v1/me/proxy-balance}. */
  public JsonObject proxyBalance() { return get("/v1/me/proxy-balance"); }

  /** Ends a session at once (404 = already ended, not an error). */
  public void endSession(String sessionId) {
    try {
      call("DELETE", apiUrl + "/v1/me/threads/" + enc(sessionId), null);
    } catch (CdpfleetException e) {
      if (e.status() != 404) throw e;
    }
  }

  private static String enc(String s) {
    return URLEncoder.encode(s, StandardCharsets.UTF_8).replace("+", "%20");
  }
}
