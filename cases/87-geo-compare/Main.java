// Java 21. Maven: com.google.code.gson:gson:2.11.0
// env: CDPFLEET_API_KEY (five countries in one call hold five threads: a paid plan)
import com.google.gson.*;
import java.net.URI;
import java.net.URLEncoder;
import java.net.http.*;
import java.nio.charset.StandardCharsets;
import java.time.*;
import java.util.*;
import java.util.regex.*;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");
  static final String API = "https://cdpfleet.com";
  static final List<String> COUNTRIES = List.of("us", "de", "gb", "jp", "br"); // the first one is the baseline the diff compares with
  static final List<String> FORMATS = List.of("markdown", "html");
  static final int MAX_WAIT = 45;
  static final String[][] PAGES = {
    {"Steam store page", "https://store.steampowered.com/app/620/Portal_2/"},
    {"Google home page", "https://www.google.com/"},
    {"Nike home page", "https://www.nike.com/"},
    {"example.com (control)", "https://example.com/"},
  };
  // Prices in the five countries' currencies: R$ before $, the symbol before or after the number.
  // Plain [0-9] and [ \u00a0] rather than \d and \s, so the pattern means the same in all five languages.
  static final Pattern PRICE = Pattern.compile(
    "R\\$[ \u00a0]?[0-9][0-9.,]*|US\\$[ \u00a0]?[0-9][0-9.,]*|£[ \u00a0]?[0-9][0-9.,]*|€[ \u00a0]?[0-9][0-9.,]*"
    + "|[0-9][0-9.,]*[ \u00a0]?€|[¥￥][ \u00a0]?[0-9][0-9,]*|[0-9][0-9,]*[ \u00a0]?円|\\$[ \u00a0]?[0-9][0-9.,]*");
  // The "accept all" button of a cookie banner, in the five countries' languages.
  static final Pattern CONSENT = Pattern.compile("Accept all|Alle akzeptieren|Aceitar todos|Aceitar tudo|すべて同意|すべて受け入れる|すべて許可",
    Pattern.CASE_INSENSITIVE | Pattern.UNICODE_CASE);
  static final String SINCE = Instant.now().minusSeconds(60).toString();
  static final HttpClient HTTP = HttpClient.newHttpClient();
  static final Gson GSON = new GsonBuilder().serializeNulls().setPrettyPrinting().disableHtmlEscaping().create();

  record Call(HttpResponse<byte[]> res, JsonObject body, long ms) {}

  static int bytes(JsonElement e) { return e != null && e.isJsonPrimitive() ? e.getAsString().getBytes(StandardCharsets.UTF_8).length : 0; }
  // A diff's size, or null when the diff is null (this country or the baseline failed).
  static JsonElement diffBytes(JsonElement e) { return e == null || e.isJsonNull() ? JsonNull.INSTANCE : new JsonPrimitive(bytes(e)); }
  static String str(JsonObject o, String k) { return o != null && o.has(k) && !o.get(k).isJsonNull() ? o.get(k).getAsString() : null; }
  static JsonObject obj(JsonObject o, String k) { return o != null && o.has(k) && o.get(k).isJsonObject() ? o.getAsJsonObject(k) : new JsonObject(); }
  static JsonArray arr(Collection<String> xs) { JsonArray a = new JsonArray(); xs.forEach(a::add); return a; }

  static List<String> prices(String text) {
    List<String> out = new ArrayList<>();
    Matcher m = PRICE.matcher(text);
    while (m.find()) out.add(m.group().replaceAll("[.,]$", "").replaceFirst("\u00a0", " "));
    return out;
  }
  static String currency(String p) {
    if (p.startsWith("R$")) return "BRL";
    if (p.contains("£")) return "GBP";
    if (p.contains("€")) return "EUR";
    if (p.matches(".*[¥￥円].*")) return "JPY";
    return "USD";
  }
  static String attr(String html, String regex) {
    Matcher m = Pattern.compile(regex, Pattern.CASE_INSENSITIVE).matcher(html);
    return m.find() ? m.group(1) : null;
  }

  // What the Function's unified diff (one country against the baseline) says.
  static JsonElement readDiff(JsonElement d) {
    if (d == null || !d.isJsonPrimitive()) return JsonNull.INSTANCE;
    String[] lines = d.getAsString().split("\n", -1);
    List<String> was = new ArrayList<>(), now = new ArrayList<>();
    int added = 0, removed = 0, hunks = 0;
    for (String l : lines) {
      if (l.startsWith("@@")) hunks++;
      else if (l.startsWith("+") && !l.startsWith("+++")) { added++; now.addAll(prices(l)); }
      else if (l.startsWith("-") && !l.startsWith("---")) { removed++; was.addAll(prices(l)); }
    }
    List<String> changes = new ArrayList<>();
    for (int i = 0; i < Math.min(was.size(), now.size()) && changes.size() < 3; i++) if (!was.get(i).equals(now.get(i))) changes.add(was.get(i) + " → " + now.get(i));
    JsonObject o = new JsonObject();
    o.addProperty("bytes", bytes(d));
    o.addProperty("hunks", hunks);
    o.addProperty("added", added);
    o.addProperty("removed", removed);
    o.add("price_changes", arr(changes));
    return o;
  }

  // The call in the account's history (its x-call-id is the session id there): weight and billed thread-seconds.
  static JsonElement billed(String id) throws Exception {
    for (int i = 0; i < 30 && id != null; i++) {
      HttpRequest req = HttpRequest.newBuilder(URI.create(API + "/v1/me/sessions?limit=200&from=" + URLEncoder.encode(SINCE, StandardCharsets.UTF_8)))
        .header("x-api-key", KEY).timeout(Duration.ofSeconds(30)).build();
      JsonObject r = JsonParser.parseString(HTTP.send(req, HttpResponse.BodyHandlers.ofString()).body()).getAsJsonObject();
      for (JsonElement e : r.getAsJsonArray("sessions")) {
        JsonObject s = e.getAsJsonObject();
        if (!id.equals(str(s, "session_id")) || str(s, "ended_at") == null) continue;
        int weight = s.get("weight").getAsInt();
        long ms = Duration.between(Instant.parse(str(s, "started_at")), Instant.parse(str(s, "ended_at"))).toMillis();
        JsonObject o = new JsonObject();
        o.addProperty("engine", str(s, "engine"));
        o.addProperty("weight", weight);
        o.addProperty("thread_seconds", Math.round(ms * weight / 100.0) / 10.0);
        return o;
      }
      Thread.sleep(2000);
    }
    return JsonNull.INSTANCE;
  }

  static Call geo(String url, List<String> countries, int maxWait, List<String> formats) throws Exception {
    JsonObject body = new JsonObject();
    body.addProperty("url", url);
    body.add("countries", arr(countries));
    body.add("formats", arr(formats));
    body.addProperty("max_wait", maxWait);
    long t = System.currentTimeMillis();
    HttpRequest req = HttpRequest.newBuilder(URI.create(API + "/v1/functions/geo")).timeout(Duration.ofSeconds(maxWait + 30))
      .header("x-api-key", KEY).header("content-type", "application/json").POST(HttpRequest.BodyPublishers.ofString(body.toString())).build();
    HttpResponse<byte[]> res = HTTP.send(req, HttpResponse.BodyHandlers.ofByteArray());
    JsonObject json;
    try { json = JsonParser.parseString(new String(res.body(), StandardCharsets.UTF_8)).getAsJsonObject(); } catch (Exception e) { json = new JsonObject(); }
    return new Call(res, json, System.currentTimeMillis() - t);
  }
  static boolean loaded(JsonObject r) {
    if (r == null || !r.has("status") || r.get("status").isJsonNull()) return false;
    int s = r.get("status").getAsInt();
    return s >= 200 && s < 400;
  }

  public static void main(String[] args) throws Exception {
    JsonArray pages = new JsonArray();
    List<String[]> ids = new ArrayList<>(); // call ids to look up: [this one, the first attempt's or null]
    for (String[] p : PAGES) { // one call per page, one after another
      Call first = geo(p[1], COUNTRIES, MAX_WAIT, FORMATS), call = first;
      // Every diff is against the first country: if that one failed, the call is retried once.
      if (call.res().statusCode() == 200 && !loaded(obj(obj(call.body(), "results"), COUNTRIES.get(0)))) call = geo(p[1], COUNTRIES, MAX_WAIT, FORMATS);
      JsonObject results = obj(call.body(), "results"), diff = obj(call.body(), "diff");
      JsonObject base = obj(results, COUNTRIES.get(0));
      JsonObject countries = new JsonObject();
      for (String c : COUNTRIES) {
        JsonObject r = obj(results, c);
        String html = Objects.requireNonNullElse(str(r, "html"), "");
        String md = Objects.requireNonNullElse(str(r, "markdown"), "");
        List<String> found = prices(md);
        String title = attr(html, "<title[^>]*>([^<]*)<");
        JsonObject o = new JsonObject();
        o.add("status", r.has("status") ? r.get("status") : JsonNull.INSTANCE);
        o.addProperty("error", str(r, "error"));
        o.addProperty("partial", r.has("partial") && r.get("partial").isJsonPrimitive() && r.get("partial").getAsBoolean());
        o.addProperty("lang", attr(html, "<html[^>]*\\slang=\"([^\"]+)\""));
        o.addProperty("url", str(r, "url")); // the final URL after redirects (only for a country that loaded)
        o.addProperty("title", title == null ? null : title.trim());
        o.add("currencies", arr(new LinkedHashSet<>(found.stream().map(Main::currency).toList())));
        o.add("prices", arr(found.subList(0, Math.min(4, found.size()))));
        o.add("consent_banner", html.isEmpty() ? JsonNull.INSTANCE : new JsonPrimitive(CONSENT.matcher(html).find()));
        o.addProperty("markdown_bytes", md.getBytes(StandardCharsets.UTF_8).length);
        o.addProperty("html_bytes", html.getBytes(StandardCharsets.UTF_8).length);
        String baseMd = str(base, "markdown");
        o.add("same_markdown_as_baseline", c.equals(COUNTRIES.get(0)) || md.isEmpty() || baseMd == null || baseMd.isEmpty() ? JsonNull.INSTANCE : new JsonPrimitive(md.equals(baseMd)));
        if (c.equals(COUNTRIES.get(0))) o.add("diff", JsonNull.INSTANCE);
        else { // a diff is a string ("" = identical), or null when this country or the baseline failed
          JsonObject d = new JsonObject();
          d.add("markdown", readDiff(obj(diff, "markdown").get(c)));
          d.add("html_bytes", diffBytes(obj(diff, "html").get(c)));
          o.add("diff", d);
        }
        countries.add(c, o);
      }
      JsonObject page = new JsonObject();
      page.addProperty("page", p[0]);
      page.addProperty("url", p[1]);
      page.addProperty("attempts", call == first ? 1 : 2);
      if (call == first) page.add("first_attempt", JsonNull.INSTANCE);
      else {
        JsonObject fa = new JsonObject(), errors = new JsonObject(), diffs = new JsonObject();
        JsonObject fresults = obj(first.body(), "results");
        for (String c : COUNTRIES) if (!loaded(obj(fresults, c))) errors.addProperty(c, str(obj(fresults, c), "error"));
        for (Map.Entry<String, JsonElement> e : obj(obj(first.body(), "diff"), "markdown").entrySet()) diffs.add(e.getKey(), diffBytes(e.getValue()));
        fa.addProperty("call_id", first.res().headers().firstValue("x-call-id").orElse(null));
        fa.add("errors", errors);
        fa.add("diffs", diffs);
        page.add("first_attempt", fa);
      }
      page.addProperty("status", call.res().statusCode());
      page.addProperty("error", str(call.body(), "error"));
      page.addProperty("seconds", Math.round(call.ms() / 100.0) / 10.0);
      page.addProperty("call_id", call.res().headers().firstValue("x-call-id").orElse(null));
      page.addProperty("warning", call.res().headers().firstValue("x-cdpfleet-warning").orElse(null));
      page.addProperty("response_bytes", call.res().body().length);
      JsonObject diffBytes = new JsonObject();
      for (String f : List.of("markdown", "html")) diffBytes.addProperty(f, obj(diff, f).entrySet().stream().mapToInt((e) -> bytes(e.getValue())).sum());
      page.add("diff_bytes", diffBytes);
      page.add("ok_countries", arr(COUNTRIES.stream().filter((c) -> loaded(obj(results, c))).toList()));
      page.add("countries", countries);
      pages.add(page);
      ids.add(new String[] {page.get("call_id").isJsonNull() ? null : page.get("call_id").getAsString(), call == first ? null : first.res().headers().firstValue("x-call-id").orElse(null)});
    }
    // What a failure looks like: the same Steam page with max_wait 1, so no country can load it in time.
    Call failed = geo("https://store.steampowered.com/app/620/Portal_2/", List.of("us", "de"), 1, List.of("markdown"));
    JsonObject failure = new JsonObject(), failedResults = new JsonObject();
    failure.addProperty("status", failed.res().statusCode());
    failure.addProperty("max_wait", 1);
    for (Map.Entry<String, JsonElement> e : obj(failed.body(), "results").entrySet()) {
      JsonObject r = new JsonObject();
      r.add("status", e.getValue().getAsJsonObject().has("status") ? e.getValue().getAsJsonObject().get("status") : JsonNull.INSTANCE);
      r.addProperty("error", str(e.getValue().getAsJsonObject(), "error"));
      failedResults.add(e.getKey(), r);
    }
    failure.add("results", failedResults);
    JsonObject failedDiff = obj(failed.body(), "diff");
    failure.add("diff", failedDiff.has("markdown") ? failedDiff.get("markdown") : JsonNull.INSTANCE);

    for (int i = 0; i < pages.size(); i++) {
      JsonObject page = pages.get(i).getAsJsonObject();
      page.add("billed", billed(ids.get(i)[0]));
      if (ids.get(i)[1] != null) page.getAsJsonObject("first_attempt").add("billed", billed(ids.get(i)[1]));
    }
    JsonObject out = new JsonObject();
    out.addProperty("language", "java");
    out.add("countries", arr(COUNTRIES));
    out.add("formats", arr(FORMATS));
    out.addProperty("max_wait", MAX_WAIT);
    out.add("pages", pages);
    out.add("failure", failure);
    out.addProperty("measured_at", LocalDate.now(ZoneOffset.UTC).toString());
    System.out.println(GSON.toJson(out));
  }
}
