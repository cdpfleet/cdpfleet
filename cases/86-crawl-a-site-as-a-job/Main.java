// Java 21. Maven: com.google.code.gson:gson:2.11.0
// env: CDPFLEET_API_KEY (a paid plan: jobs aren't on Community)
import com.google.gson.*;
import java.net.URI;
import java.net.http.*;
import java.nio.charset.StandardCharsets;
import java.nio.file.*;
import java.security.SecureRandom;
import java.time.*;
import java.util.*;
import java.util.function.Predicate;
import java.util.zip.ZipFile;
import javax.crypto.Mac;
import javax.crypto.spec.SecretKeySpec;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");
  static final String API = "https://cdpfleet.com";
  static final String SITE = "https://books.toscrape.com/"; // a sandbox made for scraping practice
  static final int MAX_DEPTH = 2, MAX_PAGES = 100, CONCURRENCY = 4, DELAY_MS = 250;
  static final int STOP_AT = 30; // stop the job once this many pages are done, then resume it
  static final Set<String> ENDED = Set.of("done", "failed", "stopped", "interrupted");
  static final HttpClient HTTP = HttpClient.newBuilder().followRedirects(HttpClient.Redirect.NORMAL).build();
  static final Gson GSON = new GsonBuilder().serializeNulls().setPrettyPrinting().disableHtmlEscaping().create();

  static HttpResponse<byte[]> send(String method, String url, JsonObject body, boolean withKey) throws Exception {
    HttpRequest.Builder b = HttpRequest.newBuilder(URI.create(url)).timeout(Duration.ofSeconds(120));
    if (withKey) b.header("x-api-key", KEY);
    if (body != null) b.header("content-type", "application/json").method(method, HttpRequest.BodyPublishers.ofString(body.toString()));
    else b.method(method, HttpRequest.BodyPublishers.noBody());
    return HTTP.send(b.build(), HttpResponse.BodyHandlers.ofByteArray());
  }
  static JsonObject json(HttpResponse<byte[]> r) {
    try { return JsonParser.parseString(new String(r.body(), StandardCharsets.UTF_8)).getAsJsonObject(); } catch (Exception e) { return new JsonObject(); }
  }
  static JsonObject api(String method, String path, JsonObject body) throws Exception { return json(send(method, API + path, body, true)); }
  static double num(JsonObject o, String k) { return o != null && o.has(k) && !o.get(k).isJsonNull() ? o.get(k).getAsDouble() : 0; }
  static String str(JsonObject o, String k) { return o != null && o.has(k) && !o.get(k).isJsonNull() ? o.get(k).getAsString() : null; }
  static JsonObject obj(JsonObject o, String k) { return o != null && o.has(k) && o.get(k).isJsonObject() ? o.getAsJsonObject(k) : new JsonObject(); }
  static double round1(double v) { return Math.round(v * 10) / 10.0; }

  // Poll the job every 3 s until `until` holds.
  static JsonObject waitFor(String id, Predicate<JsonObject> until, int seconds) throws Exception {
    JsonObject job = null;
    for (long end = System.currentTimeMillis() + seconds * 1000L; System.currentTimeMillis() < end; Thread.sleep(3000)) {
      job = api("GET", "/v1/jobs/" + id, null);
      if (until.test(job)) return job;
    }
    throw new RuntimeException("job " + id + ": still " + str(job, "status") + " after " + seconds + " s");
  }
  static List<JsonObject> stored(String id) throws Exception { // results stored so far, part by part
    List<JsonObject> rows = new ArrayList<>();
    JsonElement cursor = new JsonPrimitive(0);
    while (!cursor.isJsonNull()) {
      JsonObject r = api("GET", "/v1/jobs/" + id + "/results?cursor=" + cursor.getAsLong(), null);
      for (JsonElement e : r.getAsJsonArray("results")) rows.add(e.getAsJsonObject());
      if (r.get("next_cursor").equals(cursor)) break; // nothing new yet
      cursor = r.get("next_cursor");
    }
    return rows;
  }
  static JsonArray hookItems(String hook) throws Exception { return json(send("GET", hook, null, false)).getAsJsonArray("items"); }
  static String hmac(String secret, String data) throws Exception {
    Mac mac = Mac.getInstance("HmacSHA256");
    mac.init(new SecretKeySpec(secret.getBytes(StandardCharsets.UTF_8), "HmacSHA256"));
    return HexFormat.of().formatHex(mac.doFinal(data.getBytes(StandardCharsets.UTF_8)));
  }

  public static void main(String[] args) throws Exception {
    // 1. Map: the site's URLs in one instant call (sitemaps + the start page's links).
    long t = System.currentTimeMillis();
    JsonObject mapBody = new JsonObject();
    mapBody.addProperty("url", SITE);
    mapBody.addProperty("max_urls", 5000);
    HttpResponse<byte[]> mapRes = send("POST", API + "/v1/functions/map", mapBody, true);
    JsonObject m = json(mapRes);
    JsonObject mapOut = new JsonObject();
    mapOut.addProperty("status", mapRes.statusCode());
    mapOut.addProperty("seconds", round1((System.currentTimeMillis() - t) / 1000.0));
    int categories = 0;
    if (m.has("urls")) for (JsonElement u : m.getAsJsonArray("urls")) if (u.getAsString().contains("/catalogue/category/")) categories++;
    mapOut.addProperty("urls", m.has("urls") ? m.getAsJsonArray("urls").size() : 0);
    mapOut.add("sources", m.get("sources"));
    mapOut.addProperty("partial", mapRes.headers().firstValue("x-cdpfleet-partial").orElse(null));
    mapOut.addProperty("categories", categories);

    // 2. Start the crawl as a job: a webhook when it's done, and a zip archive at the end.
    // The receiver here is cdpfleet.com's own test inbox (keeps deliveries an hour, read back with GET);
    // use your own public https endpoint.
    byte[] token = new byte[18];
    new SecureRandom().nextBytes(token);
    String hook = API + "/e2e/hook/" + Base64.getUrlEncoder().withoutPadding().encodeToString(token);
    JsonObject crawl = new JsonObject();
    crawl.addProperty("max_depth", MAX_DEPTH);
    crawl.addProperty("max_pages", MAX_PAGES);
    crawl.addProperty("concurrency", CONCURRENCY);
    crawl.addProperty("delay_ms", DELAY_MS);
    crawl.add("formats", GSON.toJsonTree(List.of("markdown")));
    JsonObject body = crawl.deepCopy();
    body.addProperty("url", SITE);
    body.add("extract", GSON.toJsonTree(Map.of("title", "h1", "price", ".product_main .price_color")));
    body.add("deliver", JsonParser.parseString("{\"webhook\":{\"url\":\"" + hook + "\",\"events\":[\"done\"]},\"format\":\"archive\"}"));
    long tStart = System.currentTimeMillis();
    HttpResponse<byte[]> startRes = send("POST", API + "/v1/functions/crawl", body, true);
    if (startRes.statusCode() != 202) throw new RuntimeException("crawl " + startRes.statusCode() + " " + new String(startRes.body()));
    String id = str(json(startRes), "job_id");

    // 3. Watch it, and stop it part-way.
    JsonObject before = waitFor(id, j -> num(obj(j, "progress"), "done") >= STOP_AT || ENDED.contains(str(j, "status")), 600);
    JsonObject stopReply = api("DELETE", "/v1/jobs/" + id, null);
    long tStop = System.currentTimeMillis();
    JsonObject stopped = waitFor(id, j -> ENDED.contains(str(j, "status")), 300);
    long tStopped = System.currentTimeMillis();
    List<JsonObject> atStop = stored(id);
    JsonArray hooksAtStop = new JsonArray();
    for (JsonElement d : hookItems(hook)) {
      JsonObject b = JsonParser.parseString(str(d.getAsJsonObject(), "body")).getAsJsonObject();
      hooksAtStop.add(str(d.getAsJsonObject(), "event") + " " + str(obj(b, "data"), "status"));
    }
    JsonObject archiveAtStop = api("GET", "/v1/jobs/" + id + "/archive", null);

    // 4. Resume: a new run of the same job, from its checkpoint.
    long tResume = System.currentTimeMillis();
    HttpResponse<byte[]> resumeRes = send("POST", API + "/v1/jobs/" + id + "/resume", null, true);
    // 5. Wait for the second run to end.
    JsonObject done = waitFor(id, j -> num(j, "runs") >= 2 && ENDED.contains(str(j, "status")), 900);
    long tDone = System.currentTimeMillis();

    // 6a. Every result as one JSON-lines file.
    byte[] jsonl = send("GET", API + "/v1/jobs/" + id + "/results.jsonl", null, true).body();
    List<JsonObject> rows = new ArrayList<>();
    for (String line : new String(jsonl, StandardCharsets.UTF_8).strip().split("\n")) rows.add(JsonParser.parseString(line).getAsJsonObject());
    Set<String> urls = new LinkedHashSet<>(), firstRun = new HashSet<>();
    for (JsonObject r : rows) urls.add(str(r, "url"));
    for (JsonObject r : atStop) firstRun.add(str(r, "url"));
    int status200 = 0, withMarkdown = 0, booksWithPrice = 0, categoryPages = 0;
    JsonArray errors = new JsonArray(), sample = new JsonArray();
    TreeMap<String, Integer> depthCount = new TreeMap<>();
    for (JsonObject r : rows) {
      if (num(r, "status") == 200) status200++;
      if (r.has("error") && !r.get("error").isJsonNull()) {
        JsonObject e = new JsonObject();
        e.addProperty("url", str(r, "url"));
        e.add("status", r.has("status") ? r.get("status") : JsonNull.INSTANCE);
        String msg = r.get("error").isJsonPrimitive() ? r.get("error").getAsString() : r.get("error").toString();
        e.addProperty("error", msg.substring(0, Math.min(120, msg.length())));
        errors.add(e);
      }
      String md = str(r, "markdown");
      if (md != null && !md.isEmpty()) withMarkdown++;
      String price = str(obj(r, "extract"), "price");
      if (price != null) {
        booksWithPrice++;
        if (sample.size() < 2) {
          JsonObject s = new JsonObject();
          s.addProperty("url", str(r, "url"));
          s.addProperty("title", str(obj(r, "extract"), "title"));
          s.addProperty("price", price);
          sample.add(s);
        }
      }
      if (str(r, "url").contains("/catalogue/category/")) categoryPages++;
      depthCount.merge(r.has("depth") ? String.valueOf(r.get("depth").getAsInt()) : "null", 1, Integer::sum);
    }
    long secondNew = urls.stream().filter(u -> !firstRun.contains(u)).count();

    // 6b. The same results as a zip (built when the job ended).
    waitFor(id, j -> Set.of("ready", "failed").contains(String.valueOf(str(obj(j, "archive"), "status"))), 300);
    JsonObject archive = api("GET", "/v1/jobs/" + id + "/archive", null);
    byte[] zipBytes = send("GET", str(archive, "url"), null, false).body(); // a storage link, no key
    Path tmp = Files.createTempFile("crawl", ".zip");
    Files.write(tmp, zipBytes);
    List<String> names = new ArrayList<>();
    int indexLines = 0;
    try (ZipFile zf = new ZipFile(tmp.toFile())) {
      zf.stream().forEach(e -> names.add(e.getName()));
      var entry = zf.getEntry("index.jsonl");
      if (entry != null) {
        String text = new String(zf.getInputStream(entry).readAllBytes(), StandardCharsets.UTF_8).strip();
        indexLines = text.isEmpty() ? 0 : text.split("\n").length;
      }
    }
    Files.delete(tmp);

    // 7. The webhooks: every delivery is signed with the account's secret, as HMAC-SHA256(secret, "<t>.<raw body>").
    String secret = str(api("GET", "/v1/me/webhook-secret", null), "secret");
    JsonArray deliveries = new JsonArray();
    JsonObject finalData = null;
    long finalSignedAt = 0;
    for (long end = System.currentTimeMillis() + 90000; System.currentTimeMillis() < end && finalData == null;) {
      deliveries = new JsonArray();
      for (JsonElement el : hookItems(hook)) {
        JsonObject d = el.getAsJsonObject();
        Map<String, String> parts = new HashMap<>();
        for (String kv : String.valueOf(str(d, "signature")).split(",")) { String[] p = kv.split("=", 2); if (p.length == 2) parts.put(p[0], p[1]); }
        String raw = str(d, "body");
        JsonObject b = JsonParser.parseString(raw).getAsJsonObject();
        String want = hmac(secret, parts.get("t") + "." + raw);
        JsonObject o = new JsonObject();
        o.addProperty("event", str(d, "event"));
        o.addProperty("status", str(obj(b, "data"), "status"));
        o.addProperty("job_id_matches", id.equals(str(b, "job_id")));
        o.addProperty("delivery_id_is_uuid", String.valueOf(str(d, "delivery")).matches("[0-9a-f-]{36}"));
        o.addProperty("signature_valid", parts.containsKey("v1") && java.security.MessageDigest.isEqual(parts.get("v1").getBytes(), want.getBytes()));
        deliveries.add(o);
        if (finalData == null && "job.done".equals(str(d, "event")) && "done".equals(str(obj(b, "data"), "status"))) {
          finalData = obj(b, "data");
          finalSignedAt = Long.parseLong(parts.get("t"));
        }
      }
      if (finalData == null) Thread.sleep(3000);
    }
    JsonElement finalOut = JsonNull.INSTANCE;
    if (finalData != null) {
      JsonObject f = new JsonObject();
      f.add("data", finalData);
      f.addProperty("signed_seconds_after_end", finalSignedAt - Instant.parse(str(done, "ended_at")).getEpochSecond());
      finalOut = f;
    }

    // 8. What it cost: a run holds `concurrency` threads, billed_seconds is the total in thread-seconds.
    double billed = num(done, "billed_seconds");
    double runSeconds = billed / num(done, "concurrency");

    JsonObject out = new JsonObject();
    out.addProperty("language", "java");
    out.addProperty("site", SITE);
    out.add("map", mapOut);
    JsonObject crawlOut = crawl.deepCopy();
    crawlOut.add("extract", GSON.toJsonTree(List.of("title", "price")));
    crawlOut.addProperty("start_status", startRes.statusCode());
    out.add("crawl", crawlOut);
    JsonObject stop = new JsonObject();
    stop.addProperty("requested_at_done", (int) num(obj(before, "progress"), "done"));
    stop.addProperty("reply", str(stopReply, "status"));
    stop.addProperty("seconds_to_stopped", round1((tStopped - tStop) / 1000.0));
    stop.addProperty("status", str(stopped, "status"));
    stop.add("progress", stopped.get("progress"));
    stop.addProperty("stored_results", atStop.size());
    stop.add("webhooks_by_then", hooksAtStop);
    stop.addProperty("archive", str(archiveAtStop, "status"));
    out.add("stop", stop);
    JsonObject resume = new JsonObject();
    resume.addProperty("status", resumeRes.statusCode());
    resume.addProperty("reply", str(json(resumeRes), "status"));
    out.add("resume", resume);
    JsonObject job = new JsonObject();
    for (String k : List.of("status", "runs", "progress", "concurrency", "billed_seconds")) job.add(k, done.get(k));
    JsonObject res = new JsonObject();
    res.add("parts", obj(done, "results").get("parts"));
    res.add("bytes", obj(done, "results").get("bytes"));
    job.add("results", res);
    for (String k : List.of("started_at", "ended_at", "expires_at")) job.add(k, done.get(k));
    out.add("job", job);
    JsonObject rj = new JsonObject();
    rj.addProperty("lines", rows.size());
    rj.addProperty("distinct_urls", urls.size());
    rj.addProperty("bytes", jsonl.length);
    rj.addProperty("status_200", status200);
    rj.add("errors", errors);
    rj.addProperty("with_markdown", withMarkdown);
    rj.addProperty("books_with_price", booksWithPrice);
    rj.addProperty("category_pages", categoryPages);
    rj.add("depths", GSON.toJsonTree(depthCount));
    rj.addProperty("first_run", firstRun.size());
    rj.addProperty("second_run_new", secondNew);
    rj.add("sample", sample);
    out.add("results_jsonl", rj);
    JsonObject ar = new JsonObject();
    ar.addProperty("status", str(archive, "status"));
    ar.add("bytes", archive.get("bytes"));
    ar.addProperty("downloaded_bytes", zipBytes.length);
    ar.addProperty("files", names.size());
    ar.addProperty("markdown_files", names.stream().filter(x -> x.endsWith("/index.md")).count());
    ar.addProperty("extract_files", names.stream().filter(x -> x.endsWith("/extract.json")).count());
    ar.addProperty("index_lines", indexLines);
    ar.add("has", GSON.toJsonTree(List.of("index.jsonl", "errors.jsonl", "job.json").stream().filter(names::contains).toList()));
    out.add("archive", ar);
    JsonObject wh = new JsonObject();
    wh.add("deliveries", deliveries);
    wh.add("final_done", finalOut);
    out.add("webhook", wh);
    JsonObject cost = new JsonObject();
    cost.addProperty("billed_thread_seconds", billed);
    cost.addProperty("pages", rows.size());
    cost.addProperty("run_seconds", round1(runSeconds));
    cost.addProperty("pages_per_minute", round1(rows.size() / (runSeconds / 60)));
    cost.addProperty("thread_seconds_per_page", round1(billed / rows.size()));
    cost.addProperty("client_running_seconds", round1((tStopped - tStart + tDone - tResume) / 1000.0));
    out.add("cost", cost);
    out.addProperty("measured_at", LocalDate.now(ZoneOffset.UTC).toString());
    System.out.println(GSON.toJson(out));
  }
}
