// Java 21. Maven: com.google.code.gson:gson:2.11.0
// env: CDPFLEET_API_KEY (a paid plan: monitors aren't on Community)
import com.google.gson.*;
import java.net.URI;
import java.net.URLEncoder;
import java.net.http.*;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.security.SecureRandom;
import java.time.*;
import java.util.*;
import java.util.regex.*;
import javax.crypto.Mac;
import javax.crypto.spec.SecretKeySpec;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");
  static final String API = "https://cdpfleet.com";
  static final String PAGE = "https://quotes.toscrape.com/random"; // a practice site: a different quote on every load
  static final int MAX_RUNS = 5; // checks to trigger at most while waiting for a change (the same quote can come up twice)
  static final Pattern UUID_RE = Pattern.compile("^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$");
  static final HttpClient HTTP = HttpClient.newBuilder().followRedirects(HttpClient.Redirect.NORMAL).build();
  static final Gson GSON = new GsonBuilder().serializeNulls().setPrettyPrinting().disableHtmlEscaping().create();

  record Res(int status, JsonObject json, String text) {}

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
  static String get(String url) throws Exception { return send("GET", url, null, false).text(); }
  static double num(JsonObject o, String k) { return o != null && o.has(k) && !o.get(k).isJsonNull() ? o.get(k).getAsDouble() : 0; }
  static String str(JsonObject o, String k) { return o != null && o.has(k) && !o.get(k).isJsonNull() ? o.get(k).getAsString() : null; }
  static JsonObject obj(JsonObject o, String k) { return o != null && o.has(k) && o.get(k).isJsonObject() ? o.getAsJsonObject(k) : new JsonObject(); }
  static double secs(double ms) { return Math.round(ms / 100) / 10.0; }
  static double ms(String iso) { return iso == null ? 0 : Instant.parse(iso).toEpochMilli(); } // an API timestamp
  static JsonArray strings(List<String> list) { JsonArray a = new JsonArray(); list.forEach(a::add); return a; }

  // x-cdpfleet-signature: t=<unix>,v1=<hex HMAC-SHA256(secret, "<t>.<raw body>")>. Check it against the raw
  // bytes received, before parsing them, and refuse old timestamps (replays).
  static boolean verify(String secret, String header, String rawBody) throws Exception {
    Map<String, String> parts = new HashMap<>();
    for (String kv : (header == null ? "" : header).split(",")) { int i = kv.indexOf('='); if (i > 0) parts.put(kv.substring(0, i), kv.substring(i + 1)); }
    if (!parts.containsKey("t") || !parts.containsKey("v1")) return false;
    Mac mac = Mac.getInstance("HmacSHA256");
    mac.init(new SecretKeySpec(secret.getBytes(StandardCharsets.UTF_8), "HmacSHA256"));
    byte[] expected = mac.doFinal((parts.get("t") + "." + rawBody).getBytes(StandardCharsets.UTF_8));
    byte[] given;
    try { given = HexFormat.of().parseHex(parts.get("v1")); } catch (Exception e) { return false; }
    return MessageDigest.isEqual(expected, given) && Math.abs(Instant.now().getEpochSecond() - Long.parseLong(parts.get("t"))) < 300;
  }

  public static void main(String[] args) throws Exception {
    String since = Instant.now().minusSeconds(60).toString();
    // The webhook receiver here is cdpfleet.com's own test inbox: it keeps deliveries an hour, raw body and
    // headers, and reads them back with GET. Use your own public https endpoint.
    byte[] tok = new byte[18];
    new SecureRandom().nextBytes(tok);
    String hook = API + "/e2e/hook/" + Base64.getUrlEncoder().withoutPadding().encodeToString(tok);
    String secret = str(api("GET", "/v1/me/webhook-secret", null).json(), "secret");

    // 1. Create the monitor: only the quote element, as text; the tags dropped (remove) and the "(about)" link
    // label taken out of the text (ignore) before comparing.
    // every: "1h" is its own schedule; the program doesn't wait for it and triggers checks with POST …/run.
    JsonObject spec = JsonParser.parseString("""
        {"url": "%s", "format": "text", "selector": ".quote", "remove": [".tags"], "ignore": ["\\\\(about\\\\)"], "every": "1h",
         "deliver": {"webhook": {"url": "%s"}}}""".formatted(PAGE, hook)).getAsJsonObject();
    Res created = api("POST", "/v1/monitors", spec);
    if (created.status() != 201) throw new RuntimeException("create " + created.status() + " " + created.text());
    JsonObject c = created.json();
    String id = str(c, "monitor_id");
    JsonObject out = new JsonObject();
    out.addProperty("language", "java");
    out.addProperty("page", PAGE);
    try {
      boolean listed = false;
      for (JsonElement m : api("GET", "/v1/monitors", null).json().getAsJsonArray("monitors")) listed |= id.equals(str(m.getAsJsonObject(), "monitor_id"));
      JsonObject mon = new JsonObject();
      mon.addProperty("create_status", created.status());
      for (String k : List.of("every", "format", "selector")) mon.add(k, c.get(k));
      mon.add("remove", c.get("remove"));
      mon.add("ignore", c.get("ignore"));
      mon.addProperty("webhook", hook.equals(str(obj(obj(c, "deliver"), "webhook"), "url")));
      mon.add("active", c.get("active"));
      mon.addProperty("listed", listed);
      mon.addProperty("first_check_due_seconds", secs(ms(str(c, "next_run_at")) - ms(str(c, "created_at"))));
      out.add("monitor", mon);

      // 2. Check now, and again until the page has changed. Poll the monitor and the inbox every 0.5 s.
      List<JsonObject> checks = new ArrayList<>();
      List<double[]> windows = new ArrayList<>(); // [run request, last_run_at] per check
      int changeN = 0;
      double changeRun = 0, tHook = 0;
      JsonObject item = null;
      for (int n = 1; n <= MAX_RUNS; n++) {
        double tRun = System.currentTimeMillis();
        Res run = api("POST", "/v1/monitors/" + id + "/run", null);
        JsonObject m = null;
        double tStatus = 0;
        for (long end = System.currentTimeMillis() + 120000; System.currentTimeMillis() < end; Thread.sleep(500)) {
          if (tStatus == 0) {
            m = api("GET", "/v1/monitors/" + id, null).json();
            if (num(m, "checks") >= n) tStatus = System.currentTimeMillis();
          }
          if (tStatus != 0 && !"changed".equals(str(m, "last_status"))) break;
          if (item == null) {
            for (JsonElement d : JsonParser.parseString(get(hook)).getAsJsonObject().getAsJsonArray("items")) {
              if ("monitor.change".equals(str(d.getAsJsonObject(), "event"))) { item = d.getAsJsonObject(); tHook = System.currentTimeMillis(); break; }
            }
          }
          if (tStatus != 0 && item != null) break;
        }
        if (tStatus == 0) throw new RuntimeException("check " + n + ": no result after 120 s");
        JsonObject ch = new JsonObject();
        ch.addProperty("n", n);
        ch.addProperty("run_status", run.status());
        ch.add("status", m.get("last_status"));
        ch.add("error", m.get("last_error"));
        ch.addProperty("seconds_to_status", secs(tStatus - tRun));
        ch.addProperty("check_seconds", secs(ms(str(m, "last_run_at")) - tRun));
        checks.add(ch);
        windows.add(new double[] { tRun, ms(str(m, "last_run_at")) });
        if ("changed".equals(str(m, "last_status"))) { changeN = n; changeRun = tRun; break; }
      }

      // 3. The webhook: verify it, then compare it with the change as the API keeps it.
      JsonArray deliveries = JsonParser.parseString(get(hook)).getAsJsonObject().getAsJsonArray("items");
      String changeId = null;
      if (item != null) {
        String raw = str(item, "body");
        JsonObject body = JsonParser.parseString(raw).getAsJsonObject();
        JsonObject data = obj(body, "data");
        changeId = str(data, "change_id");
        Matcher tm = Pattern.compile("t=(\\d+)").matcher(str(item, "signature"));
        long t = tm.find() ? Long.parseLong(tm.group(1)) : 0;
        JsonObject full = api("GET", "/v1/monitors/" + id + "/changes/" + changeId, null).json();
        String before = str(full, "before_url") != null ? get(str(full, "before_url")) : null;
        String after = get(str(full, "after_url"));
        String snapshot = get(str(data, "snapshot_url"));
        String diff = str(data, "diff");
        String[] lines = diff.split("\n", -1);
        List<String> removed = new ArrayList<>(), added = new ArrayList<>();
        for (String l : lines) {
          if (l.startsWith("-") && !l.startsWith("---")) removed.add(l.substring(1));
          if (l.startsWith("+") && !l.startsWith("+++")) added.add(l.substring(1));
        }
        List<String> keys = new ArrayList<>(data.keySet());
        Collections.sort(keys);
        JsonObject wh = new JsonObject();
        wh.addProperty("deliveries", deliveries.size());
        wh.add("event_header", item.get("event"));
        wh.add("type", body.get("type"));
        wh.addProperty("delivery_id_is_uuid", UUID_RE.matcher(Objects.toString(str(item, "delivery"), "")).matches());
        wh.addProperty("signature_valid", verify(secret, str(item, "signature"), raw));
        wh.addProperty("tampered_body_rejected", !verify(secret, str(item, "signature"), raw.replaceFirst("\"added\":", "\"added\": ")));
        wh.addProperty("monitor_id_matches", id.equals(str(data, "monitor_id")));
        wh.add("url", data.get("url"));
        wh.add("keys", strings(keys));
        wh.addProperty("seconds_run_to_seen", secs(tHook - changeRun));
        wh.addProperty("seconds_run_to_signed", t - (long) changeRun / 1000);
        wh.addProperty("signed_within_1s_of_change", Math.abs(t * 1000 - ms(str(data, "at"))) < 1000);
        out.add("webhook", wh);
        JsonObject chg = new JsonObject();
        chg.addProperty("after_runs", changeN);
        chg.addProperty("change_id", changeId);
        chg.add("added", data.get("added"));
        chg.add("removed", data.get("removed"));
        chg.addProperty("diff_bytes", diff.getBytes(StandardCharsets.UTF_8).length);
        chg.addProperty("diff_lines", lines.length);
        chg.add("removed_lines", strings(removed));
        chg.add("added_lines", strings(added));
        chg.addProperty("diff_has_ignored_text", Pattern.compile("Tags:|\\(about\\)").matcher(diff).find());
        chg.addProperty("api_diff_equal", diff.equals(str(full, "diff")));
        chg.addProperty("api_counts_equal", num(full, "added") == num(data, "added") && num(full, "removed") == num(data, "removed"));
        chg.addProperty("snapshot_equals_after", snapshot.equals(after));
        if (before != null) chg.addProperty("before_lines", before.split("\n", -1).length); else chg.add("before_lines", JsonNull.INSTANCE);
        chg.addProperty("after_lines", after.split("\n", -1).length);
        chg.addProperty("after_text", after);
        out.add("change", chg);
      }

      // 4. The history: every change, newest first, with a preview of its diff.
      JsonArray changes = api("GET", "/v1/monitors/" + id + "/changes", null).json().getAsJsonArray("changes");
      List<String> ids = new ArrayList<>();
      for (JsonElement e : changes) ids.add(str(e.getAsJsonObject(), "change_id"));
      JsonObject m = api("GET", "/v1/monitors/" + id, null).json();
      JsonObject hist = new JsonObject();
      hist.addProperty("changes", ids.size());
      hist.add("change_ids", strings(ids));
      hist.addProperty("newest_is_webhook_change", !ids.isEmpty() && ids.get(0).equals(changeId));
      hist.add("monitor_checks", m.get("checks"));
      hist.add("monitor_changes", m.get("changes"));
      hist.add("last_status", m.get("last_status"));
      hist.addProperty("next_run_minutes_after_last_check", Math.round((ms(str(m, "next_run_at")) - ms(str(m, "last_run_at"))) / 60000));
      out.add("history", hist);

      // 5. Pause it (PATCH active: false): no more scheduled checks, history kept.
      JsonObject patch = new JsonObject();
      patch.addProperty("active", false);
      Res paused = api("PATCH", "/v1/monitors/" + id, patch);
      JsonObject pause = new JsonObject();
      pause.addProperty("status", paused.status());
      pause.add("active", paused.json().get("active"));
      pause.add("next_run_at", paused.json().has("next_run_at") ? paused.json().get("next_run_at") : JsonNull.INSTANCE);
      out.add("pause", pause);

      // 6. What the checks cost. Each check is a scrape the service runs as you, billed like any call: it has no
      // x-call-id you can see, but it is in the account's session history as engine "function:scrape".
      Thread.sleep(3000);
      JsonArray sessions = api("GET", "/v1/me/sessions?limit=200&from=" + URLEncoder.encode(since, StandardCharsets.UTF_8), null).json().getAsJsonArray("sessions");
      JsonArray billing = new JsonArray();
      for (int i = 0; i < checks.size(); i++) {
        double[] w = windows.get(i);
        List<JsonObject> inside = new ArrayList<>();
        for (JsonElement e : sessions) {
          JsonObject s = e.getAsJsonObject();
          double st = ms(str(s, "started_at"));
          if ("function:scrape".equals(str(s, "engine")) && st >= w[0] - 2000 && st <= w[1]) inside.add(s);
        }
        inside.sort(Comparator.comparingDouble(s -> ms(str(s, "started_at"))));
        JsonObject b = new JsonObject();
        b.addProperty("check", i + 1);
        b.addProperty("sessions_in_window", inside.size());
        JsonObject s = inside.isEmpty() ? null : inside.get(0); // the first scrape to start after the run request
        b.add("engine", s == null ? JsonNull.INSTANCE : s.get("engine"));
        b.add("weight", s == null ? JsonNull.INSTANCE : s.get("weight"));
        if (s != null && str(s, "ended_at") != null) {
          double dur = ms(str(s, "ended_at")) - ms(str(s, "started_at"));
          b.addProperty("seconds", secs(dur));
          b.addProperty("thread_seconds", secs(dur * num(s, "weight")));
        } else { b.add("seconds", JsonNull.INSTANCE); b.add("thread_seconds", JsonNull.INSTANCE); }
        billing.add(b);
      }
      out.add("billing", billing);
      JsonArray checksOut = new JsonArray();
      checks.forEach(checksOut::add);
      out.add("checks", checksOut);
    } finally {
      // 7. Delete it, whatever happened above.
      Res del = api("DELETE", "/v1/monitors/" + id, null);
      Res gone = api("GET", "/v1/monitors/" + id, null);
      JsonObject d = new JsonObject();
      d.addProperty("status", del.status());
      d.addProperty("deleted", del.json().has("deleted") && del.json().get("deleted").getAsBoolean());
      d.addProperty("get_after", gone.status());
      out.add("deleted", d);
    }
    out.addProperty("measured_at", LocalDate.now(ZoneOffset.UTC).toString());
    System.out.println(GSON.toJson(out));
  }
}
