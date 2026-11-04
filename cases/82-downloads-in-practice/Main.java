// Maven: io.modelcontextprotocol.sdk:mcp:2.1.0, com.google.code.gson:gson:2.11.0
// env: CDPFLEET_API_KEY (a paid plan: kept downloads aren't on Community), PROXY_URL
import com.google.gson.*;
import io.modelcontextprotocol.client.McpClient;
import io.modelcontextprotocol.client.McpSyncClient;
import io.modelcontextprotocol.client.transport.HttpClientStreamableHttpTransport;
import io.modelcontextprotocol.spec.McpSchema;
import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.net.http.WebSocket;
import java.nio.ByteBuffer;
import java.security.MessageDigest;
import java.time.Duration;
import java.util.*;
import java.util.concurrent.*;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.regex.*;
import java.util.stream.*;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");
  static final String PROXY = System.getenv("PROXY_URL");
  static final String API = "https://cdpfleet.com";
  static final HttpClient HTTP = HttpClient.newBuilder().connectTimeout(Duration.ofSeconds(30)).build();
  // Seeded test files: the same URL always returns the same bytes, so a copy can be checked
  // against the original fetched directly from here.
  static final List<String> PERSON_FILES = List.of("https://httpbingo.org/bytes/204800?seed=82", "https://httpbingo.org/bytes/65536?seed=7");
  static final String AGENT_FILE = "https://httpbingo.org/bytes/102400?seed=79";

  // One HTTP request; the API key goes only to cdpfleet.
  static HttpResponse<byte[]> request(String method, String url, JsonObject body, boolean withKey) throws Exception {
    var b = HttpRequest.newBuilder(URI.create(url)).timeout(Duration.ofSeconds(120))
        .method(method, body == null ? HttpRequest.BodyPublishers.noBody() : HttpRequest.BodyPublishers.ofString(body.toString()));
    if (withKey) b.header("x-api-key", KEY);
    if (body != null) b.header("content-type", "application/json");
    return HTTP.send(b.build(), HttpResponse.BodyHandlers.ofByteArray());
  }

  static JsonObject json(HttpResponse<byte[]> res) { return JsonParser.parseString(new String(res.body())).getAsJsonObject(); }

  static String sha256(byte[] b) throws Exception {
    return HexFormat.of().formatHex(MessageDigest.getInstance("SHA-256").digest(b));
  }

  // Poll the account API until the session's kept files are listed; ms from `since`.
  static Object[] listed(String sessionId, int count, long since) throws Exception {
    while (true) {
      var files = json(request("GET", API + "/v1/me/downloads?session_id=" + sessionId, null, true)).getAsJsonArray("files");
      if (files.size() >= count || System.currentTimeMillis() - since > 120000) return new Object[] { files, System.currentTimeMillis() - since };
      Thread.sleep(250);
    }
  }

  // Fetch each kept file through its private link and compare it with what the list said and,
  // for seeded files, with the original.
  static List<Map<String, Object>> fetchKept(JsonArray files, Map<String, String> links) throws Exception {
    var sorted = new ArrayList<JsonObject>();
    files.forEach(e -> sorted.add(e.getAsJsonObject()));
    sorted.sort(Comparator.comparingLong((JsonObject f) -> f.get("bytes").getAsLong()).reversed());
    var out = new ArrayList<Map<String, Object>>();
    for (var f : sorted) {
      String name = f.get("name").getAsString();
      String url = links.get(name);
      String how = "from the downloads tool";
      if (url == null) {
        var link = json(request("GET", API + "/v1/me/downloads/" + f.get("id").getAsString() + "/url", null, true));
        url = link.get("url").getAsString();
        how = "GET /v1/me/downloads/{id}/url (" + link.get("expires_in_s").getAsInt() + " s)";
      }
      var res = request("GET", url, null, false);
      String source = f.get("source_url").getAsString();
      var c = new LinkedHashMap<String, Object>();
      c.put("name", name); c.put("bytes", f.get("bytes").getAsLong()); c.put("sha256", f.get("sha256").getAsString()); c.put("source_url", source);
      c.put("link", how);
      c.put("fetched_status", res.statusCode()); c.put("fetched_type", res.headers().firstValue("content-type").orElse(null));
      c.put("fetched_bytes", res.body().length); c.put("fetched_sha256", sha256(res.body()));
      byte[] original = Pattern.compile("[?&]seed=").matcher(source).find() ? request("GET", source, null, false).body() : null;
      c.put("original_bytes", original == null ? null : original.length);
      c.put("original_sha256", original == null ? null : sha256(original));
      out.add(c);
    }
    return out;
  }

  // The viewer: JPEG frames (binary) and JSON messages; "pages" says which URL is on screen.
  static final List<String> SEEN = new CopyOnWriteArrayList<>();
  static final AtomicInteger FRAMES = new AtomicInteger();

  static class Viewer implements WebSocket.Listener {
    final StringBuilder text = new StringBuilder();

    public CompletionStage<?> onBinary(WebSocket ws, ByteBuffer data, boolean last) {
      if (last) FRAMES.incrementAndGet();
      ws.request(1);
      return null;
    }

    public CompletionStage<?> onText(WebSocket ws, CharSequence data, boolean last) {
      text.append(data);
      if (last) {
        JsonObject m = JsonParser.parseString(text.toString()).getAsJsonObject();
        text.setLength(0);
        if (m.has("type") && m.get("type").getAsString().equals("pages")) {
          for (JsonElement e : m.getAsJsonArray("pages")) {
            JsonObject pg = e.getAsJsonObject();
            if (pg.get("targetId").equals(m.get("current")) && pg.has("url") && !pg.get("url").isJsonNull()) {
              String url = pg.get("url").getAsString();
              if (SEEN.isEmpty() || !SEEN.get(SEEN.size() - 1).equals(url)) SEEN.add(url);
            }
          }
        }
      }
      ws.request(1);
      return null;
    }
  }

  // ---- 1. A person in Launch interactive: no client, the server opens the start page and
  //         a person drives it through a control live link (a scripted stand-in here).
  static Map<String, Object> person() throws Exception {
    var body = new JsonObject();
    body.addProperty("proxy", PROXY);
    body.addProperty("headless", true);
    body.addProperty("live_view", true);
    body.addProperty("record", false);
    body.addProperty("downloads", true);
    body.addProperty("start_url", "https://httpbingo.org/");
    body.addProperty("inactivity_timeout", 60);
    var res = request("POST", "https://starter.cdpfleet.com/chromium/session", body, true);
    if (res.statusCode() != 200) throw new RuntimeException("launch " + res.statusCode() + " " + new String(res.body()));
    var session = json(res);
    String sessionId = session.get("sessionId").getAsString();
    var liveBody = new JsonObject();
    liveBody.addProperty("session_id", sessionId);
    liveBody.addProperty("mode", "control");
    var live = json(request("POST", "https://" + URI.create(session.get("wsUrl").getAsString()).getHost() + "/admin/session/live", liveBody, true));

    WebSocket viewer = HTTP.newWebSocketBuilder().buildAsync(URI.create(live.get("live_url").getAsString()), new Viewer()).join();
    for (int i = 0; i < 60 && !SEEN.contains("https://httpbingo.org/"); i++) Thread.sleep(500);
    boolean startShown = SEEN.contains("https://httpbingo.org/");

    // The person pastes two file links into the address bar, a few seconds apart.
    for (String url : PERSON_FILES) {
      var m = new JsonObject();
      m.addProperty("type", "navigate");
      m.addProperty("url", url);
      viewer.sendText(m.toString(), true).join();
      Thread.sleep(3000);
    }
    String pageAfter = SEEN.isEmpty() ? null : SEEN.get(SEEN.size() - 1);
    viewer.sendClose(WebSocket.NORMAL_CLOSURE, "").join();

    // Closing the dashboard's viewer ends a Launch interactive session: DELETE /v1/me/threads/{id}.
    long t = System.currentTimeMillis();
    int endStatus = request("DELETE", API + "/v1/me/threads/" + sessionId, null, true).statusCode();
    long ended = System.currentTimeMillis();
    var l = listed(sessionId, PERSON_FILES.size(), ended);
    var out = new LinkedHashMap<String, Object>();
    out.put("launch_downloads", session.get("downloads").getAsBoolean());
    out.put("resource_class", session.get("resource_class").getAsString());
    out.put("live_protocol", live.get("protocol").getAsString());
    out.put("start_page_shown", startShown);
    out.put("frames", FRAMES.get());
    out.put("pasted", PERSON_FILES);
    out.put("page_after", pageAfter);
    out.put("end_status", endStatus);
    out.put("end_ms", ended - t);
    out.put("listed_after_ms", l[1]);
    out.put("files", fetchKept((JsonArray) l[0], Map.of()));
    return out;
  }

  // ---- 2. An AI agent through the hosted MCP server, authenticated with the API key.
  static Map<String, Object> agent() throws Exception {
    var transport = HttpClientStreamableHttpTransport.builder(API).endpoint("/mcp")
        .httpRequestCustomizer((builder, method, uri, body, ctx) -> builder.header("Authorization", "Bearer " + KEY))
        .build();
    McpSyncClient client = McpClient.sync(transport).requestTimeout(Duration.ofSeconds(120)).build();
    client.initialize();
    var calls = new ArrayList<Map<String, Object>>();
    java.util.function.BiFunction<String, Map<String, Object>, String> call = (name, args) -> {
      long t = System.currentTimeMillis();
      var r = client.callTool(new McpSchema.CallToolRequest(name, args));
      String text = r.content().stream().filter(c -> c instanceof McpSchema.TextContent)
          .map(c -> ((McpSchema.TextContent) c).text()).collect(Collectors.joining("\n"));
      String first = text.split("\n", 2)[0];
      var e = new LinkedHashMap<String, Object>();
      e.put("tool", name); e.put("ms", System.currentTimeMillis() - t); e.put("error", Boolean.TRUE.equals(r.isError()));
      e.put("text", first.length() > 160 ? first.substring(0, 160) : first);
      calls.add(e);
      return text;
    };

    String launched = call.apply("launch_browser", Map.of("browser", "chromium", "headless", true, "proxy", PROXY, "record", false, "downloads", true));
    var sm = Pattern.compile("session ([0-9a-f-]{36})").matcher(launched);
    String sessionId = sm.find() ? sm.group(1) : null;
    call.apply("navigate", Map.of("url", "https://httpbingo.org/"));
    call.apply("click", Map.of("role", "link", "name", "/bytes/:n")); // the index page's example link: 1,024 random bytes
    call.apply("navigate", Map.of("url", AGENT_FILE)); // a file link opened directly
    call.apply("evaluate", Map.of("expression", "location.href"));
    Thread.sleep(2000); // an assistant's next call comes seconds later; let the downloads finish
    call.apply("close_browser", Map.of());

    // Ask the downloads tool until both files are there.
    long closed = System.currentTimeMillis();
    String text = "";
    int polls = 0;
    while (System.currentTimeMillis() - closed < 120000) {
      polls++;
      text = call.apply("downloads", Map.of());
      if (text.split(" bytes, sha256 ", -1).length - 1 >= 2) break;
      Thread.sleep(500);
    }
    long toolMs = System.currentTimeMillis() - closed;
    client.closeGracefully(); // ends the MCP session (DELETE)

    // "<name> — <bytes> bytes, sha256 <hex>" and a private link on the next line.
    var links = new LinkedHashMap<String, String>();
    var toolFiles = new ArrayList<Map<String, Object>>();
    var m = Pattern.compile("^(.+) — (\\d+) bytes, sha256 ([0-9a-f]{64})\\n {2}(https:\\S+)", Pattern.MULTILINE).matcher(text);
    while (m.find()) {
      links.put(m.group(1), m.group(4));
      var f = new LinkedHashMap<String, Object>();
      f.put("name", m.group(1)); f.put("bytes", Long.parseLong(m.group(2))); f.put("sha256", m.group(3)); f.put("link_host", URI.create(m.group(4)).getHost());
      toolFiles.add(f);
    }
    var l = listed(sessionId, 2, System.currentTimeMillis());
    var kept = calls.stream().filter(c -> !c.get("tool").equals("downloads")).collect(Collectors.toCollection(ArrayList::new));
    var last = calls.get(calls.size() - 1);
    last.put("polls", polls);
    kept.add(last);
    var out = new LinkedHashMap<String, Object>();
    out.put("session_id_found", sessionId != null);
    out.put("calls", kept);
    out.put("tool_listed_after_ms", toolMs);
    out.put("first_downloads_call", calls.stream().filter(c -> c.get("tool").equals("downloads")).findFirst().get().get("text"));
    out.put("tool_files", toolFiles);
    out.put("files", fetchKept((JsonArray) l[0], links));
    return out;
  }

  public static void main(String[] args) throws Exception {
    var result = new LinkedHashMap<String, Object>();
    result.put("language", "java");
    result.put("person", person());
    result.put("agent", agent());
    result.put("measured_at", java.time.LocalDate.now(java.time.ZoneOffset.UTC).toString());
    System.out.println(new GsonBuilder().setPrettyPrinting().disableHtmlEscaping().serializeNulls().create().toJson(result));
    System.exit(0); // the SDK's HTTP client keeps non-daemon threads
  }
}
