// Java 21. Maven: com.google.code.gson:gson:2.11.0
// env: CDPFLEET_API_KEY (a paid plan: jobs aren't on Community)
import com.google.gson.*;
import java.io.*;
import java.net.URI;
import java.net.URLEncoder;
import java.net.http.*;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.security.SecureRandom;
import java.time.*;
import java.util.*;
import java.util.regex.*;
import java.util.stream.*;
import javax.crypto.Mac;
import javax.crypto.spec.SecretKeySpec;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");
  static final String API = "https://cdpfleet.com";
  // A public page that changes on its own: it asks its server for the ISS position about once a second
  // (XHR to /w/ajax/realtime) and rewrites a small table with it.
  static final String PAGE = "https://wheretheiss.at/";
  static final int STOP_AFTER_S = 40; // DELETE the job this long after starting it
  static final List<String> ENDED = List.of("done", "failed", "stopped", "interrupted");
  static final List<String> TYPES = List.of("xhr", "fetch", "ws", "dom");
  static final Pattern UUID_RE = Pattern.compile("^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$");
  static final HttpClient HTTP = HttpClient.newBuilder().followRedirects(HttpClient.Redirect.NORMAL).build();
  static final Gson GSON = new GsonBuilder().serializeNulls().setPrettyPrinting().disableHtmlEscaping().create();

  record Res(int status, JsonObject json, String text) {}
  record Live(JsonObject ev, long received) {}

  static Res send(String method, String url, JsonObject body, boolean withKey) throws Exception {
    HttpRequest.Builder b = HttpRequest.newBuilder(URI.create(url)).timeout(Duration.ofSeconds(120));
    if (withKey) b.header("x-api-key", KEY);
    if (body != null) b.header("content-type", "application/json").method(method, HttpRequest.BodyPublishers.ofString(body.toString()));
    else b.method(method, HttpRequest.BodyPublishers.noBody());
    HttpResponse<byte[]> r = HTTP.send(b.build(), HttpResponse.BodyHandlers.ofByteArray());
    String text = new String(r.body(), StandardCharsets.UTF_8);
    JsonObject json;
    try { json = JsonParser.parseString(text).getAsJsonObject(); } catch (Exception e) { json = new JsonObject(); }
    return new Res(r.statusCode(), json, text);
  }
  static Res api(String method, String path, JsonObject body) throws Exception { return send(method, API + path, body, true); }
  static String str(JsonObject o, String k) { return o != null && o.has(k) && !o.get(k).isJsonNull() ? o.get(k).getAsString() : null; }
  static JsonObject obj(JsonObject o, String k) { return o != null && o.has(k) && o.get(k).isJsonObject() ? o.getAsJsonObject(k) : null; }
  static double secs(double ms) { return Math.round(ms / 100) / 10.0; }
  static double round(double x, int d) { double p = Math.pow(10, d); return Math.round(x * p) / p; }
  static long ms(String iso) { return Instant.parse(iso).toEpochMilli(); } // an API timestamp
  static String key(JsonObject e) { return str(e, "type") + " " + str(e, "at") + " " + (e.has("url") ? str(e, "url") : str(e, "selector")); }

  // x-cdpfleet-signature: t=<unix>,v1=<hex HMAC-SHA256(secret, "<t>.<raw body>")>, checked on the raw bytes.
  static boolean verify(String secret, String header, String rawBody) throws Exception {
    Map<String, String> parts = new HashMap<>();
    for (String kv : (header == null ? "" : header).split(",")) { int i = kv.indexOf('='); if (i > 0) parts.put(kv.substring(0, i), kv.substring(i + 1)); }
    if (!parts.containsKey("t") || !parts.containsKey("v1")) return false;
    Mac mac = Mac.getInstance("HmacSHA256");
    mac.init(new SecretKeySpec(secret.getBytes(StandardCharsets.UTF_8), "HmacSHA256"));
    byte[] expected = mac.doFinal((parts.get("t") + "." + rawBody).getBytes(StandardCharsets.UTF_8));
    byte[] given;
    try { given = HexFormat.of().parseHex(parts.get("v1")); } catch (Exception e) { return false; }
    return MessageDigest.isEqual(expected, given);
  }

  // Great-circle distance in km.
  static double km(double lat1, double lon1, double lat2, double lon2) {
    double h = Math.pow(Math.sin(Math.toRadians(lat2 - lat1) / 2), 2)
        + Math.cos(Math.toRadians(lat1)) * Math.cos(Math.toRadians(lat2)) * Math.pow(Math.sin(Math.toRadians(lon2 - lon1) / 2), 2);
    return 2 * 6371 * Math.asin(Math.sqrt(h));
  }

  static JsonObject byType(List<JsonObject> events) {
    JsonObject o = new JsonObject();
    for (String t : TYPES) { long n = events.stream().filter(e -> t.equals(str(e, "type"))).count(); if (n > 0) o.addProperty(t, n); }
    return o;
  }

  public static void main(String[] args) throws Exception {
    // The webhook receiver here is cdpfleet.com's own test inbox: it keeps deliveries an hour (raw body and
    // headers) and reads them back with GET. Use your own public https endpoint.
    byte[] tok = new byte[18];
    new SecureRandom().nextBytes(tok);
    String hook = API + "/e2e/hook/" + Base64.getUrlEncoder().withoutPadding().encodeToString(tok);
    String secret = str(api("GET", "/v1/me/webhook-secret", null).json(), "secret");

    // 1. Start the watch: XHR/fetch calls matching the glob, and changes under the selector, with a webhook.
    JsonObject watch = JsonParser.parseString("""
        {"url": "%s", "browser": "chrome", "duration": 300, "reload_every": 15,
         "filter": {"url": ["*/w/ajax/realtime*"], "selector": ".span3 table.table-striped", "types": ["xhr", "fetch", "dom"]}}"""
        .formatted(PAGE)).getAsJsonObject(); // duration 300: a safety net, the program stops the job itself
    JsonObject body = watch.deepCopy();
    body.add("deliver", JsonParser.parseString("{\"webhook\": {\"url\": \"" + hook + "\", \"events\": [\"event\", \"done\"]}}"));
    long t0 = System.currentTimeMillis();
    Res start = api("POST", "/v1/functions/watch", body);
    if (start.status() != 202) throw new RuntimeException("watch " + start.status() + " " + start.text());
    String id = str(start.json(), "job_id");

    // 2. Follow it live on the SSE stream; stop it with DELETE after STOP_AFTER_S seconds.
    JsonObject stop = new JsonObject();
    Thread stopper = Thread.ofVirtual().start(() -> {
      try {
        Thread.sleep(STOP_AFTER_S * 1000L);
        long t = System.currentTimeMillis();
        Res r = api("DELETE", "/v1/jobs/" + id, null);
        synchronized (stop) { stop.addProperty("at", t); stop.addProperty("status", r.status()); stop.addProperty("reply", str(r.json(), "status")); }
      } catch (InterruptedException e) { /* the job ended first */ } catch (Exception e) { throw new RuntimeException(e); }
    });

    List<Live> live = new ArrayList<>();
    Map<String, Integer> seen = new LinkedHashMap<>();
    for (String n : List.of("progress", "part", "event", "done")) seen.put(n, 0);
    JsonObject doneData = null;
    HttpRequest sseReq = HttpRequest.newBuilder(URI.create(API + "/v1/jobs/" + id + "/events"))
        .header("x-api-key", KEY).header("accept", "text/event-stream").GET().build();
    HttpResponse<InputStream> sse = HTTP.send(sseReq, HttpResponse.BodyHandlers.ofInputStream());
    try (BufferedReader reader = new BufferedReader(new InputStreamReader(sse.body(), StandardCharsets.UTF_8))) {
      String name = null;
      StringBuilder data = new StringBuilder();
      for (String line; (line = reader.readLine()) != null;) {
        if (!line.isEmpty()) {
          if (line.startsWith("event:")) name = line.substring(6).trim();
          else if (line.startsWith("data:")) data.append(data.length() > 0 ? "\n" : "").append(line.substring(5).trim());
          continue;
        }
        if (name != null) { // a blank line ends a message; a keep-alive comment has no event name
          seen.merge(name, 1, Integer::sum);
          JsonObject payload = data.length() > 0 ? JsonParser.parseString(data.toString()).getAsJsonObject() : new JsonObject();
          if (name.equals("event")) for (JsonElement ev : payload.getAsJsonArray("events")) live.add(new Live(ev.getAsJsonObject(), System.currentTimeMillis()));
          if (name.equals("done")) { doneData = payload; break; }
        }
        name = null;
        data.setLength(0);
      }
    }
    stopper.interrupt();
    stopper.join();
    long tEnd = System.currentTimeMillis();

    // 3. The job as it ended, and its events as stored.
    JsonObject job = api("GET", "/v1/jobs/" + id, null).json();
    for (int i = 0; i < 20 && !ENDED.contains(str(job, "status")); i++) { Thread.sleep(2000); job = api("GET", "/v1/jobs/" + id, null).json(); }
    List<JsonObject> stored = new ArrayList<>();
    for (long cursor = 0; ; ) {
      JsonObject r = api("GET", "/v1/jobs/" + id + "/results?cursor=" + cursor, null).json();
      r.getAsJsonArray("results").forEach(x -> stored.add(x.getAsJsonObject()));
      if (r.get("next_cursor").isJsonNull() || r.get("next_cursor").getAsLong() == cursor) break;
      cursor = r.get("next_cursor").getAsLong();
    }

    // 4. The webhook deliveries: job.event batches (at most one per 2 s) and job.done. Wait for job.done.
    List<JsonObject> items = new ArrayList<>();
    for (long end = System.currentTimeMillis() + 90000; System.currentTimeMillis() < end; Thread.sleep(2000)) {
      items.clear();
      send("GET", hook, null, false).json().getAsJsonArray("items").forEach(x -> items.add(x.getAsJsonObject()));
      if (items.stream().anyMatch(d -> "job.done".equals(str(d, "event")))) break;
    }
    List<JsonObject> batches = items.stream().filter(d -> "job.event".equals(str(d, "event"))).toList();
    JsonObject doneHook = items.stream().filter(d -> "job.done".equals(str(d, "event"))).findFirst().orElse(null);
    List<JsonObject> hookEvents = new ArrayList<>();
    List<Long> sent = new ArrayList<>();
    List<Double> delays = new ArrayList<>();
    int maxPerBatch = 0;
    for (JsonObject d : batches) {
      JsonObject b = JsonParser.parseString(str(d, "body")).getAsJsonObject();
      JsonArray evs = b.getAsJsonObject("data").getAsJsonArray("events");
      evs.forEach(x -> hookEvents.add(x.getAsJsonObject()));
      maxPerBatch = Math.max(maxPerBatch, evs.size());
      sent.add(ms(str(b, "created_at")));
      // How long after its newest event each batch was signed (the signature's t, whole seconds).
      Matcher m = Pattern.compile("t=(\\d+)").matcher(str(d, "signature"));
      long newest = 0;
      for (JsonElement x : evs) newest = Math.max(newest, ms(str(x.getAsJsonObject(), "at")));
      if (m.find()) delays.add(Long.parseLong(m.group(1)) - newest / 1000.0);
    }
    Collections.sort(sent);
    Collections.sort(delays);
    long minGap = Long.MAX_VALUE;
    for (int i = 1; i < sent.size(); i++) minGap = Math.min(minGap, sent.get(i) - sent.get(i - 1));
    Set<String> storedKeys = stored.stream().map(Main::key).collect(Collectors.toSet());
    Set<String> hookKeys = hookEvents.stream().map(Main::key).collect(Collectors.toSet());

    // 5. What the events say: the ISS position from each XHR response, the table text from each DOM change.
    List<double[]> positions = new ArrayList<>(); // at, lat, lon
    List<String> times = new ArrayList<>();
    for (JsonObject e : stored) {
      if (!"xhr".equals(str(e, "type")) || str(e, "response_body") == null) continue; // an XHR still in flight when the page reloads has no body
      JsonObject d = JsonParser.parseString(str(e, "response_body")).getAsJsonObject().getAsJsonObject("data");
      positions.add(new double[] { ms(str(e, "at")), d.get("lat").getAsDouble(), d.get("lon").getAsDouble() });
      times.add(str(d, "time"));
    }
    List<Double> lags = live.stream().map(x -> (x.received() - ms(str(x.ev(), "at"))) / 1000.0).sorted().toList();
    List<JsonObject> dom = stored.stream().filter(e -> "dom".equals(str(e, "type"))).toList();

    // 6. The bill: a watch runs in your own browser session, one thread for a headless Chrome.
    String since = Instant.ofEpochMilli(t0 - 60000).toString();
    JsonArray sessions = api("GET", "/v1/me/sessions?from=" + URLEncoder.encode(since, StandardCharsets.UTF_8) + "&limit=200", null).json().getAsJsonArray("sessions");
    String sessionId = str(job, "session_id");
    JsonObject session = null;
    for (JsonElement s : sessions) {
      JsonObject so = s.getAsJsonObject();
      if (sessionId != null && (sessionId.equals(str(so, "session_id")) || sessionId.equals(str(so, "id")))) session = so;
    }

    JsonObject out = new JsonObject();
    out.addProperty("language", "java");
    out.addProperty("page", PAGE);
    JsonObject w = watch.deepCopy();
    w.addProperty("stop_after_s", STOP_AFTER_S);
    w.addProperty("start_status", start.status());
    out.add("watch", w);

    JsonObject s = new JsonObject();
    s.addProperty("events", live.size());
    s.add("by_type", byType(live.stream().map(Live::ev).toList()));
    s.add("messages", GSON.toJsonTree(seen));
    s.addProperty("first_event_seconds", live.isEmpty() ? null : secs(live.get(0).received() - t0));
    for (String t : List.of("xhr", "dom")) {
      Live f = live.stream().filter(x -> t.equals(str(x.ev(), "type"))).findFirst().orElse(null);
      s.addProperty("first_" + t + "_seconds", f == null ? null : secs(f.received() - t0));
    }
    if (lags.isEmpty()) s.add("lag_seconds", JsonNull.INSTANCE);
    else {
      JsonObject l = new JsonObject();
      l.addProperty("min", round(lags.get(0), 2));
      l.addProperty("median", round(lags.get(lags.size() / 2), 2));
      l.addProperty("max", round(lags.get(lags.size() - 1), 2));
      s.add("lag_seconds", l);
    }
    s.addProperty("done_status", str(doneData, "status"));
    s.addProperty("stream_seconds", secs(tEnd - t0));
    out.add("sse", s);

    if (stop.has("at")) {
      JsonObject st = new JsonObject();
      long at = stop.get("at").getAsLong();
      st.addProperty("after_seconds", secs(at - t0));
      st.add("status", stop.get("status"));
      st.add("reply", stop.get("reply"));
      st.addProperty("seconds_to_ended", secs(ms(str(job, "ended_at")) - at));
      out.add("stop", st);
    } else out.add("stop", JsonNull.INSTANCE);

    JsonObject j = new JsonObject();
    for (String k : List.of("status", "own_session", "concurrency", "runs", "progress", "billed_seconds")) j.add(k, job.has(k) ? job.get(k) : JsonNull.INSTANCE);
    j.addProperty("run_seconds", secs(ms(str(job, "ended_at")) - ms(str(job, "started_at"))));
    JsonObject res = obj(job, "results");
    if (res != null) { JsonObject r = new JsonObject(); r.add("parts", res.get("parts")); r.add("bytes", res.get("bytes")); j.add("results", r); }
    else j.add("results", JsonNull.INSTANCE);
    j.add("error", job.has("error") ? job.get("error") : JsonNull.INSTANCE);
    out.add("job", j);

    if (session != null) {
      JsonObject se = new JsonObject();
      se.add("engine", session.get("engine"));
      se.add("weight", session.get("weight"));
      se.addProperty("seconds", secs(ms(str(session, "ended_at")) - ms(str(session, "started_at"))));
      out.add("session", se);
    } else out.add("session", JsonNull.INSTANCE);

    JsonObject so = new JsonObject();
    so.addProperty("events", stored.size());
    so.add("by_type", byType(stored));
    so.addProperty("same_as_live", stored.size() == live.size() && live.stream().allMatch(x -> storedKeys.contains(key(x.ev()))));
    so.addProperty("xhr_urls_match_glob", stored.stream().filter(e -> "xhr".equals(str(e, "type")))
        .allMatch(e -> str(e, "url").contains("/w/ajax/realtime") && e.get("status").getAsInt() == 200 && "GET".equals(str(e, "method"))));
    so.addProperty("xhr_without_body", stored.stream().filter(e -> "xhr".equals(str(e, "type")) && str(e, "response_body") == null).count());
    so.addProperty("positions", positions.size());
    so.addProperty("distinct_positions", positions.stream().map(p -> p[1] + "," + p[2]).distinct().count());
    if (!positions.isEmpty()) {
      double[] f = positions.get(0);
      double[] l = positions.get(positions.size() - 1);
      for (Object[] fl : new Object[][] { { "first", f, times.get(0) }, { "last", l, times.get(times.size() - 1) } }) {
        double[] p = (double[]) fl[1];
        JsonObject o = new JsonObject();
        o.addProperty("lat", round(p[1], 3));
        o.addProperty("lon", round(p[2], 3));
        o.addProperty("time", (String) fl[2]);
        so.add((String) fl[0], o);
      }
      double dist = km(f[1], f[2], l[1], l[2]);
      so.addProperty("ground_km", round(dist, 1));
      so.addProperty("ground_km_per_s", l[0] > f[0] ? round(dist / ((l[0] - f[0]) / 1000), 2) : null);
    } else { so.add("first", JsonNull.INSTANCE); so.add("last", JsonNull.INSTANCE); so.add("ground_km", JsonNull.INSTANCE); so.add("ground_km_per_s", JsonNull.INSTANCE); }
    so.addProperty("dom_events", dom.size());
    so.addProperty("dom_text_sample", dom.isEmpty() ? null : str(dom.get(0), "text").replaceAll("\\s+", " ").trim());
    so.addProperty("dom_html_bytes", dom.isEmpty() ? null : str(dom.get(0), "html").getBytes(StandardCharsets.UTF_8).length);
    out.add("stored", so);

    JsonObject wh = new JsonObject();
    wh.addProperty("deliveries", items.size());
    wh.addProperty("inbox_full", items.size() >= 20);
    JsonObject byEvent = new JsonObject();
    for (JsonObject d : items) byEvent.addProperty(str(d, "event"), (byEvent.has(str(d, "event")) ? byEvent.get(str(d, "event")).getAsInt() : 0) + 1);
    wh.add("by_event", byEvent);
    int valid = 0;
    for (JsonObject d : items) if (verify(secret, str(d, "signature"), str(d, "body"))) valid++;
    wh.addProperty("signatures_valid", valid);
    wh.addProperty("tampered_body_rejected", !items.isEmpty() && !verify(secret, str(items.get(0), "signature"), str(items.get(0), "body") + " "));
    wh.addProperty("delivery_ids_unique_uuids", items.stream().map(d -> str(d, "delivery")).distinct().count() == items.size()
        && items.stream().allMatch(d -> str(d, "delivery") != null && UUID_RE.matcher(str(d, "delivery")).matches()));
    wh.addProperty("job_id_matches", items.stream().allMatch(d -> id.equals(str(JsonParser.parseString(str(d, "body")).getAsJsonObject(), "job_id"))));
    wh.addProperty("events", hookEvents.size());
    wh.add("events_by_type", byType(hookEvents));
    wh.addProperty("events_all_stored", hookEvents.stream().allMatch(e -> storedKeys.contains(key(e))));
    wh.addProperty("stored_not_in_webhooks", stored.stream().filter(e -> !hookKeys.contains(key(e))).count());
    long newestSent = hookEvents.stream().mapToLong(e -> ms(str(e, "at"))).max().orElse(0);
    wh.addProperty("not_sent_are_the_last", stored.stream().filter(e -> !hookKeys.contains(key(e))).allMatch(e -> ms(str(e, "at")) >= newestSent));
    if (delays.isEmpty()) wh.add("signed_after_newest_event_seconds", JsonNull.INSTANCE);
    else {
      JsonObject dl = new JsonObject();
      dl.addProperty("min", round(delays.get(0), 1));
      dl.addProperty("max", round(delays.get(delays.size() - 1), 1));
      wh.add("signed_after_newest_event_seconds", dl);
    }
    wh.addProperty("min_gap_seconds", sent.size() > 1 ? secs(minGap) : null);
    wh.addProperty("max_events_per_batch", maxPerBatch);
    if (doneHook != null) {
      JsonObject dd = JsonParser.parseString(str(doneHook, "body")).getAsJsonObject().getAsJsonObject("data");
      JsonObject o = new JsonObject();
      o.add("status", dd.get("status"));
      o.add("progress", dd.get("progress"));
      o.add("error", dd.has("error") ? dd.get("error") : JsonNull.INSTANCE);
      wh.add("done", o);
    } else wh.add("done", JsonNull.INSTANCE);
    out.add("webhook", wh);
    out.addProperty("measured_at", LocalDate.now(ZoneOffset.UTC).toString());
    System.out.println(GSON.toJson(out));
  }
}
