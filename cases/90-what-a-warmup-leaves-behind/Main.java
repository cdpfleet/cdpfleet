// Java 21. Maven: com.microsoft.playwright:playwright:1.60.0, com.google.code.gson:gson:2.11.0
// env: CDPFLEET_API_KEY (a paid plan: jobs and profiles aren't on Community), PROXY_URL_US (a sticky US exit)
import com.google.gson.*;
import com.microsoft.playwright.*;
import com.microsoft.playwright.options.*;
import java.net.URI;
import java.net.URLEncoder;
import java.net.http.*;
import java.nio.charset.StandardCharsets;
import java.security.SecureRandom;
import java.time.*;
import java.util.*;
import java.util.stream.*;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");
  static final String PROXY = System.getenv("PROXY_URL_US");
  static final String API = "https://cdpfleet.com";
  static final String NAME = "case90-java-" + hex(4); // a profile of its own for each run
  // Three ordinary public sites: an encyclopedia, a scraping sandbox, a news front page.
  static final List<String> SITES = List.of("https://en.wikipedia.org/wiki/Main_Page", "https://books.toscrape.com/", "https://www.theguardian.com/us");
  static final int STOP_AFTER_S = 150;
  static final List<String> ENDED = List.of("done", "failed", "stopped", "interrupted");
  static final double DAY = 86400;
  static final HttpClient HTTP = HttpClient.newBuilder().followRedirects(HttpClient.Redirect.NORMAL).build();
  static final Gson GSON = new GsonBuilder().serializeNulls().setPrettyPrinting().disableHtmlEscaping().create();

  record Res(int status, JsonObject json, String text) {}
  // A cookie as the API and Playwright both describe it.
  record Ck(String name, String domain, String path, double expires, boolean httpOnly, String sameSite) {
    String key() { return name + "|" + domain + "|" + path; }
  }
  record Look(String sessionId, List<Ck> cookiesAtStart, JsonArray origins, double seconds) {}

  static String hex(int n) { byte[] b = new byte[n]; new SecureRandom().nextBytes(b); return HexFormat.of().formatHex(b); }
  static Res api(String method, String path, JsonObject body) throws Exception {
    HttpRequest.Builder b = HttpRequest.newBuilder(URI.create(API + path)).timeout(Duration.ofSeconds(90)).header("x-api-key", KEY);
    if (body != null) b.header("content-type", "application/json").method(method, HttpRequest.BodyPublishers.ofString(body.toString()));
    else b.method(method, HttpRequest.BodyPublishers.noBody());
    HttpResponse<String> r = HTTP.send(b.build(), HttpResponse.BodyHandlers.ofString());
    JsonObject json;
    try { json = JsonParser.parseString(r.body()).getAsJsonObject(); } catch (Exception e) { json = new JsonObject(); }
    return new Res(r.statusCode(), json, r.body());
  }
  static String str(JsonObject o, String k) { return o != null && o.has(k) && !o.get(k).isJsonNull() ? o.get(k).getAsString() : null; }
  static double secs(double ms) { return Math.round(ms / 100) / 10.0; }
  static long ms(String iso) { return Instant.parse(iso).toEpochMilli(); } // an API timestamp
  static void sleep(long ms) throws InterruptedException { Thread.sleep(ms); }

  static JsonObject profileInfo() throws Exception {
    JsonObject r = api("GET", "/v1/me/profiles", null).json();
    if (!r.has("profiles")) return null;
    for (JsonElement p : r.getAsJsonArray("profiles")) if (NAME.equals(str(p.getAsJsonObject(), "name"))) return p.getAsJsonObject();
    return null;
  }

  // The site a cookie or a host belongs to: its last two labels (three under co.uk and the like).
  static String site(String host) {
    String[] l = host.replaceFirst("^\\.", "").split("\\.");
    int n = l.length >= 2 && (l[l.length - 2] + "." + l[l.length - 1]).matches("^(co|com|org|net|ac|gov)\\.[a-z]{2}$") ? 3 : 2;
    return String.join(".", Arrays.copyOfRange(l, Math.max(0, l.length - n), l.length));
  }

  // Cookies by site, first- vs third-party (first = a site the warmup visited as a page), and how long they last.
  static JsonObject summarize(List<Ck> cookies, List<String> firstParty, double now) {
    Map<String, Integer> bySite = new HashMap<>();
    for (Ck c : cookies) bySite.merge(site(c.domain()), 1, Integer::sum);
    List<Map.Entry<String, Integer>> third = bySite.entrySet().stream().filter(e -> !firstParty.contains(e.getKey()))
        .sorted(Comparator.comparing((Map.Entry<String, Integer> e) -> -e.getValue()).thenComparing(Map.Entry::getKey)).toList();
    List<Double> left = cookies.stream().filter(c -> c.expires() > 0).map(c -> (c.expires() - now) / DAY).toList();
    long first = cookies.stream().filter(c -> firstParty.contains(site(c.domain()))).count();
    JsonObject o = new JsonObject();
    o.addProperty("total", cookies.size());
    o.addProperty("sites", bySite.size());
    o.addProperty("first_party", first);
    o.addProperty("third_party", cookies.size() - first);
    o.addProperty("third_party_sites", third.size());
    JsonObject byFirst = new JsonObject();
    for (String s : firstParty) byFirst.addProperty(s, bySite.getOrDefault(s, 0));
    o.add("by_first_party_site", byFirst);
    JsonArray top = new JsonArray();
    third.stream().limit(5).forEach(e -> top.add(e.getKey() + " " + e.getValue()));
    o.add("top_third_party", top);
    o.addProperty("session", cookies.size() - left.size());
    o.addProperty("http_only", cookies.stream().filter(Ck::httpOnly).count());
    o.addProperty("same_site_none", cookies.stream().filter(c -> "None".equals(c.sameSite())).count());
    JsonObject exp = new JsonObject();
    exp.addProperty("under_1_day", left.stream().filter(d -> d < 1).count());
    exp.addProperty("under_30_days", left.stream().filter(d -> d >= 1 && d < 30).count());
    exp.addProperty("under_1_year", left.stream().filter(d -> d >= 30 && d < 365).count());
    exp.addProperty("a_year_or_more", left.stream().filter(d -> d >= 365).count());
    exp.add("max_days", left.isEmpty() ? JsonNull.INSTANCE : new JsonPrimitive(Math.round(Collections.max(left))));
    o.add("expiry", exp);
    o.addProperty("same_key_twice", cookies.size() - cookies.stream().map(Ck::key).distinct().count()); // e.g. Firefox's per-site (partitioned) copies
    return o;
  }

  static List<Ck> fromApi(JsonArray arr) {
    List<Ck> list = new ArrayList<>();
    for (JsonElement e : arr) {
      JsonObject c = e.getAsJsonObject();
      list.add(new Ck(str(c, "name"), str(c, "domain"), str(c, "path"), c.has("expires") ? c.get("expires").getAsDouble() : -1,
          c.has("httpOnly") && c.get("httpOnly").getAsBoolean(), str(c, "sameSite")));
    }
    return list;
  }

  static String sameSite(SameSiteAttribute s) {
    if (s == null) return null;
    return switch (s) { case NONE -> "None"; case LAX -> "Lax"; case STRICT -> "Strict"; };
  }

  static Browser launch(Playwright pw, JsonElement profile, String[] sessionId) throws Exception {
    JsonObject body = new JsonObject();
    body.addProperty("proxy", PROXY);
    body.addProperty("headless", true);
    if (profile != null) body.add("profile", profile);
    for (int attempt = 1; ; attempt++) {
      HttpResponse<String> res = HTTP.send(HttpRequest.newBuilder(URI.create("https://starter.cdpfleet.com/camoufox/session"))
          .timeout(Duration.ofSeconds(120)).header("x-api-key", KEY).header("content-type", "application/json")
          .POST(HttpRequest.BodyPublishers.ofString(body.toString())).build(), HttpResponse.BodyHandlers.ofString());
      if (res.statusCode() == 200 || res.statusCode() == 201) {
        JsonObject s = JsonParser.parseString(res.body()).getAsJsonObject();
        sessionId[0] = str(s, "sessionId");
        return pw.firefox().connect(str(s, "wsUrl"), new BrowserType.ConnectOptions().setHeaders(Map.of("x-api-key", KEY)));
      }
      if (res.statusCode() != 503 || attempt == 5) throw new RuntimeException("launch " + res.statusCode() + " " + res.body());
      sleep(Long.parseLong(res.headers().firstValue("retry-after").orElse("5")) * 1000);
    }
  }

  // What one later session sees: the cookies it starts with, then for each warmed site one request for its
  // /robots.txt — the Cookie header that first request carries is what the site gets from this browser, and
  // the plain-text page lets us read the origin's localStorage and IndexedDB with none of the site's scripts run.
  static Look inspect(Playwright pw, JsonElement profile) throws Exception {
    String[] sid = new String[1];
    Browser browser = launch(pw, profile, sid);
    long t = System.currentTimeMillis();
    try {
      // With a profile, its context is browser.contexts().get(0); without one, a fresh context.
      BrowserContext context = profile != null ? browser.contexts().get(0) : browser.newContext();
      Page page = context.pages().isEmpty() ? context.newPage() : context.pages().get(0);
      List<Ck> atStart = context.cookies().stream().map(c -> new Ck(c.name, c.domain, c.path, c.expires == null ? -1 : c.expires,
          Boolean.TRUE.equals(c.httpOnly), sameSite(c.sameSite))).toList();
      JsonArray origins = new JsonArray();
      for (String url : SITES) {
        URI u = URI.create(url);
        String origin = u.getScheme() + "://" + u.getHost();
        Response res = null;
        for (int attempt = 1; attempt <= 3 && res == null; attempt++) {
          try { res = page.navigate(origin + "/robots.txt", new Page.NavigateOptions().setTimeout(60000)); }
          catch (PlaywrightException e) { if (attempt == 3) throw e; sleep(2000); }
        }
        String header = res.request().allHeaders().getOrDefault("cookie", "");
        List<String> names = header.isEmpty() ? List.of() : Arrays.stream(header.split("; ")).map(c -> c.contains("=") ? c.substring(0, c.indexOf('=')) : c).toList();
        JsonObject storage = JsonParser.parseString(GSON.toJson(page.evaluate(
            "async () => ({ local_storage: localStorage.length, indexeddb: (await indexedDB.databases()).length })"))).getAsJsonObject();
        JsonObject o = new JsonObject();
        o.addProperty("origin", origin);
        o.addProperty("status", res.status());
        o.addProperty("cookie_header_cookies", names.size());
        JsonArray first8 = new JsonArray();
        names.stream().limit(8).forEach(first8::add);
        o.add("cookie_header_names", first8);
        for (String k : List.of("local_storage", "indexeddb")) o.addProperty(k, storage.get(k).getAsInt());
        origins.add(o);
        sleep(3000);
      }
      return new Look(sid[0], atStart, origins, secs(System.currentTimeMillis() - t));
    } finally {
      browser.close();
    }
  }

  public static void main(String[] args) throws Exception {
    JsonObject warmup = JsonParser.parseString("""
        {"browser": "camoufox", "duration": 600,
         "behavior": {"scroll": true, "mouse": true, "typing": false, "dwell_s": [4, 10], "pages_per_site": [2, 4]}}""").getAsJsonObject();
    // duration is a safety net: the program stops the job itself. No typing: the warmup would otherwise type into search boxes now and then.
    warmup.addProperty("url", SITES.get(0));
    JsonArray more = new JsonArray();
    SITES.subList(1, SITES.size()).forEach(more::add);
    warmup.add("sites", more);
    warmup.addProperty("profile", NAME);

    JsonObject out = new JsonObject();
    out.addProperty("measured_at", LocalDate.now(ZoneOffset.UTC).toString());
    JsonArray sites = new JsonArray();
    SITES.forEach(sites::add);
    out.add("sites", sites);
    JsonObject shown = new JsonObject();
    for (String k : List.of("url", "sites", "browser", "profile", "duration", "behavior")) shown.add(k, warmup.get(k));
    shown.addProperty("profile", "case90-java-…");
    shown.addProperty("stop_after_s", STOP_AFTER_S);
    out.add("warmup", shown);
    String id = null;
    try {
      // 1. Start the warmup into a new profile, on a sticky US exit.
      long t0 = System.currentTimeMillis();
      JsonObject body = warmup.deepCopy();
      JsonObject launch = new JsonObject();
      launch.addProperty("headless", true);
      launch.addProperty("proxy", PROXY);
      body.add("launch", launch);
      Res start = api("POST", "/v1/functions/warmup", body);
      if (start.status() != 202) throw new RuntimeException("warmup " + start.status() + " " + start.text());
      id = str(start.json(), "job_id");
      shown.addProperty("start_status", start.status());

      // 2. Follow its progress; stop it after STOP_AFTER_S seconds.
      List<JsonObject> samples = new ArrayList<>();
      JsonObject job;
      JsonObject stop = null;
      long stopAt = 0;
      while (true) {
        sleep(5000);
        job = api("GET", "/v1/jobs/" + id, null).json();
        JsonObject p = job.has("progress") && job.get("progress").isJsonObject() ? job.getAsJsonObject("progress").deepCopy() : new JsonObject();
        p.addProperty("s", Math.round((System.currentTimeMillis() - t0) / 1000.0));
        samples.add(p);
        if (ENDED.contains(str(job, "status"))) break;
        if (stop == null && System.currentTimeMillis() - t0 >= STOP_AFTER_S * 1000L) {
          Res r = api("DELETE", "/v1/jobs/" + id, null);
          stop = new JsonObject();
          stop.addProperty("after_seconds", secs(System.currentTimeMillis() - t0));
          stop.addProperty("status", r.status());
          stop.addProperty("reply", str(r.json(), "status"));
          stopAt = System.currentTimeMillis();
        }
        if (System.currentTimeMillis() - t0 > (STOP_AFTER_S + 120) * 1000L) throw new RuntimeException("job still " + str(job, "status"));
      }
      long endedAt = ms(str(job, "ended_at"));
      JsonObject j = new JsonObject();
      j.addProperty("status", str(job, "status"));
      j.add("own_session", job.get("own_session"));
      j.add("runs", job.get("runs"));
      j.add("progress", job.get("progress"));
      j.add("error", job.get("error"));
      j.add("billed_seconds", job.get("billed_seconds"));
      j.addProperty("run_seconds", secs(endedAt - ms(str(job, "started_at"))));
      if (stop != null) stop.addProperty("seconds_to_ended", secs(endedAt - stopAt));
      j.add("stop", stop);
      JsonArray kept = new JsonArray();
      for (int i = 0; i < samples.size(); i++) {
        JsonObject p = samples.get(i);
        if (!p.has("visits")) continue;
        if (i < samples.size() - 1 && p.get("visits").equals(samples.get(i + 1).get("visits"))) continue;
        JsonObject k = new JsonObject();
        for (String f : List.of("s", "visits", "sites", "challenges")) k.add(f, p.get(f));
        kept.add(k);
      }
      j.add("progress_samples", kept);
      out.add("job", j);

      // 3. The visit history: the job's results log every page it browsed.
      List<JsonObject> lines = new ArrayList<>();
      for (JsonElement cursor = new JsonPrimitive(0); !cursor.isJsonNull(); ) {
        JsonObject r = api("GET", "/v1/jobs/" + id + "/results?cursor=" + cursor.getAsString(), null).json();
        for (JsonElement e : r.getAsJsonArray("results")) lines.add(e.getAsJsonObject());
        if (r.get("next_cursor").equals(cursor)) break;
        cursor = r.get("next_cursor");
      }
      List<JsonObject> visits = lines.stream().filter(l -> "visit".equals(str(l, "type"))).toList();
      List<String> firstParty = visits.stream().map(v -> site(URI.create(str(v, "url")).getHost())).distinct().sorted().toList();
      JsonObject vs = new JsonObject();
      vs.addProperty("count", visits.size());
      vs.addProperty("challenged", visits.stream().filter(v -> v.has("challenged") && v.get("challenged").getAsBoolean()).count());
      JsonObject bySite = new JsonObject();
      for (String s : firstParty) bySite.addProperty(s, visits.stream().filter(v -> site(URI.create(str(v, "url")).getHost()).equals(s)).count());
      vs.add("by_site", bySite);
      JsonArray statuses = new JsonArray();
      visits.stream().map(v -> v.get("status").getAsInt()).distinct().sorted().forEach(statuses::add);
      vs.add("statuses", statuses);
      if (visits.isEmpty()) vs.add("dwell_s", JsonNull.INSTANCE);
      else {
        JsonObject d = new JsonObject();
        d.addProperty("min", visits.stream().mapToInt(v -> v.get("dwell_s").getAsInt()).min().getAsInt());
        d.addProperty("max", visits.stream().mapToInt(v -> v.get("dwell_s").getAsInt()).max().getAsInt());
        vs.add("dwell_s", d);
      }
      JsonArray paths = new JsonArray();
      visits.forEach(v -> { URI u = URI.create(str(v, "url")); paths.add(u.getHost() + u.getRawPath()); });
      vs.add("paths", paths);
      vs.addProperty("cookie_lines", lines.stream().filter(l -> "cookies".equals(str(l, "type"))).count());
      out.add("visits", vs);

      // 4. The cookies it left: GET /v1/jobs/{id}/cookies (after the end, the last set it delivered).
      Res ck = api("GET", "/v1/jobs/" + id + "/cookies", null);
      List<Ck> jobCookies = ck.json().has("cookies") ? fromApi(ck.json().getAsJsonArray("cookies")) : List.of();
      JsonObject jc = new JsonObject();
      jc.addProperty("status", ck.status());
      jc.add("live", ck.json().get("live"));
      jc.addProperty("at_vs_end_seconds", secs(ms(str(ck.json(), "at")) - endedAt));
      summarize(jobCookies, firstParty, System.currentTimeMillis() / 1000.0).entrySet().forEach(e -> jc.add(e.getKey(), e.getValue()));
      out.add("job_cookies", jc);

      // 5. The profile, saved after the job's browser exited.
      JsonObject p = null;
      for (int i = 0; i < 60; i++) {
        p = profileInfo();
        if (p != null && p.get("version").getAsInt() >= 1 && !p.get("in_use").getAsBoolean()) break;
        sleep(2000);
      }
      if (p == null) out.add("profile", JsonNull.INSTANCE);
      else {
        JsonObject pr = new JsonObject();
        pr.add("engine", p.get("engine"));
        pr.add("version", p.get("version"));
        pr.add("bytes", p.get("bytes"));
        pr.addProperty("saved_after_end_seconds", secs(ms(str(p, "saved_at")) - endedAt));
        out.add("profile", pr);
      }

      // 6. A later session on the profile (read-only: it sees the profile and doesn't change it), then a fresh one.
      Look later, fresh;
      try (Playwright pw = Playwright.create()) {
        JsonObject ro = new JsonObject();
        ro.addProperty("id", NAME);
        ro.addProperty("mode", "readonly");
        later = inspect(pw, ro);
        fresh = inspect(pw, null);
      }
      Set<String> keys = later.cookiesAtStart().stream().map(Ck::key).collect(Collectors.toSet());
      Set<String> jobKeys = jobCookies.stream().map(Ck::key).collect(Collectors.toSet());
      List<Ck> session = jobCookies.stream().filter(c -> !(c.expires() > 0)).toList();
      JsonObject l = new JsonObject();
      l.add("cookies_at_start", summarize(later.cookiesAtStart(), firstParty, System.currentTimeMillis() / 1000.0));
      l.addProperty("job_cookies_restored", jobKeys.stream().filter(keys::contains).count());
      l.addProperty("job_cookies_distinct", jobKeys.size());
      l.addProperty("session_cookies_restored", session.stream().filter(c -> keys.contains(c.key())).count());
      l.addProperty("session_cookies_in_job", session.size());
      l.addProperty("not_in_job", keys.stream().filter(k -> !jobKeys.contains(k)).count());
      l.add("origins", later.origins());
      l.addProperty("seconds", later.seconds());
      out.add("later", l);
      JsonObject f = new JsonObject();
      f.addProperty("cookies_at_start", fresh.cookiesAtStart().size());
      f.add("origins", fresh.origins());
      f.addProperty("seconds", fresh.seconds());
      out.add("fresh", f);

      // 7. The bill: the warmup's own session and the two later sessions, from the session history.
      String since = URLEncoder.encode(Instant.ofEpochMilli(t0 - 60000).toString(), StandardCharsets.UTF_8);
      List<String> wanted = Arrays.asList(str(job, "session_id"), later.sessionId(), fresh.sessionId());
      JsonArray sessions = new JsonArray();
      for (int i = 0; i < 15; i++) {
        JsonObject r = api("GET", "/v1/me/sessions?from=" + since + "&limit=200", null).json();
        sessions = r.has("sessions") ? r.getAsJsonArray("sessions") : new JsonArray();
        final JsonArray ss = sessions;
        if (wanted.stream().allMatch(w -> ss.asList().stream().anyMatch(x -> w != null && w.equals(sid(x.getAsJsonObject())) && str(x.getAsJsonObject(), "ended_at") != null))) break;
        sleep(2000);
      }
      JsonObject bill = new JsonObject();
      JsonObject w = new JsonObject();
      w.add("billed_seconds", job.get("billed_seconds"));
      w.add("session", bill(sessions, str(job, "session_id")));
      bill.add("warmup", w);
      bill.add("later", bill(sessions, later.sessionId()));
      bill.add("fresh", bill(sessions, fresh.sessionId()));
      out.add("bill", bill);
    } finally {
      // Stop the job if something failed early, then delete the profile once nothing holds it.
      if (id != null && !out.has("job")) api("DELETE", "/v1/jobs/" + id, null);
      for (int i = 0; i < 60; i++) {
        JsonObject info = profileInfo();
        if (info == null || !info.get("in_use").getAsBoolean()) break;
        sleep(2000);
      }
      Res del = api("DELETE", "/v1/me/profiles/" + NAME, null);
      JsonObject c = new JsonObject();
      c.addProperty("delete_status", del.status());
      c.addProperty("deleted", del.status() == 200 && del.json().has("deleted") && del.json().get("deleted").getAsBoolean() && profileInfo() == null);
      out.add("cleanup", c);
    }
    System.out.println(GSON.toJson(out));
  }

  static String sid(JsonObject s) { return str(s, "session_id") != null ? str(s, "session_id") : str(s, "id"); }

  static JsonElement bill(JsonArray sessions, String sessionId) {
    for (JsonElement e : sessions) {
      JsonObject s = e.getAsJsonObject();
      if (sessionId == null || !sessionId.equals(sid(s))) continue;
      JsonObject o = new JsonObject();
      o.add("engine", s.get("engine"));
      o.add("weight", s.get("weight"));
      String ended = str(s, "ended_at");
      if (ended == null) o.add("seconds", JsonNull.INSTANCE);
      else o.addProperty("seconds", secs(ms(ended) - ms(str(s, "started_at"))));
      return o;
    }
    return JsonNull.INSTANCE;
  }
}
