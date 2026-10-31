// Maven: io.modelcontextprotocol.sdk:mcp:2.1.0, com.google.code.gson:gson:2.11.0
// env: CDPFLEET_API_KEY, PROXY_URL
import com.google.gson.*;
import io.modelcontextprotocol.client.McpClient;
import io.modelcontextprotocol.client.McpSyncClient;
import io.modelcontextprotocol.client.transport.HttpClientStreamableHttpTransport;
import io.modelcontextprotocol.spec.McpSchema;
import java.time.Duration;
import java.util.*;
import java.util.regex.*;
import java.util.stream.*;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");
  static final String PROXY = System.getenv("PROXY_URL");
  static final String EXTRACT = "() => [...document.querySelectorAll('article.product_pod')].map((a) => ({ title: a.querySelector('h3 a').title, price: a.querySelector('.price_color').textContent }))";
  static final Pattern SESSION = Pattern.compile("session ([0-9a-f-]{36})");

  // One MCP session over Streamable HTTP; a script authenticates with its API key as a bearer token.
  // `seen` collects the MCP session ids the client sends (the transport adds the header itself).
  static McpSyncClient connect(Set<String> seen) {
    var transport = HttpClientStreamableHttpTransport.builder("https://cdpfleet.com")
        .endpoint("/mcp")
        .httpRequestCustomizer((builder, method, uri, body, ctx) -> {
          builder.header("Authorization", "Bearer " + KEY);
          builder.copy().build().headers().firstValue("mcp-session-id").ifPresent(seen::add);
        })
        .build();
    var client = McpClient.sync(transport).requestTimeout(Duration.ofSeconds(120)).build();
    client.initialize();
    return client;
  }

  record Result(String text, boolean error) {}

  static Result call(McpSyncClient client, List<Map<String, Object>> log, String name, Map<String, Object> args) {
    long t = System.currentTimeMillis();
    var r = client.callTool(new McpSchema.CallToolRequest(name, args));
    String text = r.content().stream().filter(c -> c instanceof McpSchema.TextContent)
        .map(c -> ((McpSchema.TextContent) c).text()).collect(Collectors.joining("\n"));
    boolean error = Boolean.TRUE.equals(r.isError());
    var entry = new LinkedHashMap<String, Object>();
    entry.put("tool", name); entry.put("ms", System.currentTimeMillis() - t); entry.put("error", error);
    log.add(entry);
    return new Result(text, error);
  }

  // A shared proxy drops the odd connection: one retry of a failed page load.
  static Result navigate(McpSyncClient client, List<Map<String, Object>> log, String url) {
    var r = call(client, log, "navigate", Map.of("url", url));
    return r.error() ? call(client, log, "navigate", Map.of("url", url)) : r;
  }

  static String firstLine(String s) { return s.split("\n", 2)[0]; }
  static Map<String, Object> launchArgs() { return Map.of("browser", "chromium", "headless", true, "proxy", PROXY, "record", false); }
  static Map<String, Object> entry(String tool, long ms) { var e = new LinkedHashMap<String, Object>(); e.put("tool", tool); e.put("ms", ms); e.put("error", false); return e; }

  // ---- 1. One MCP session for the whole job, as Claude or Cursor keep one.
  static Map<String, Object> oneSession() {
    var log = new ArrayList<Map<String, Object>>();
    var out = new LinkedHashMap<String, Object>();
    var seen = new HashSet<String>();
    long t = System.currentTimeMillis();
    var client = connect(seen);
    log.add(entry("initialize", System.currentTimeMillis() - t));
    t = System.currentTimeMillis();
    var tools = client.listTools().tools();
    log.add(entry("tools/list", System.currentTimeMillis() - t));
    out.put("mcp_session", !seen.isEmpty());
    out.put("tools", tools.stream().map(McpSchema.Tool::name).sorted().toList());

    var launched = call(client, log, "launch_browser", launchArgs()).text();
    var m = Pattern.compile("^Launched (\\S+) (\\S+) \\((\\w+), (\\d+) thread").matcher(launched);
    if (m.find()) { out.put("browser", m.group(1)); out.put("version", m.group(2)); out.put("resource_class", m.group(3)); out.put("weight", Integer.parseInt(m.group(4))); }
    var sm = SESSION.matcher(launched);
    String sid = sm.find() ? sm.group(1) : "";
    out.put("session_id", sid);
    out.put("navigate", firstLine(navigate(client, log, "https://books.toscrape.com/").text()));
    out.put("snapshot_chars", call(client, log, "snapshot", Map.of()).text().length());
    out.put("snapshot_list_chars", call(client, log, "snapshot", Map.of("selector", "ol.row")).text().length());
    out.put("html_chars", Integer.parseInt(call(client, log, "evaluate", Map.of("expression", "document.documentElement.outerHTML.length")).text().trim()));
    var books = JsonParser.parseString(call(client, log, "evaluate", Map.of("expression", EXTRACT)).text()).getAsJsonArray();
    out.put("books", books.size());
    out.put("first_book", Map.of("title", books.get(0).getAsJsonObject().get("title").getAsString(), "price", books.get(0).getAsJsonObject().get("price").getAsString()));
    out.put("closed", call(client, log, "close_browser", Map.of()).text().startsWith("Session " + sid + " ended"));
    var after = call(client, log, "navigate", Map.of("url", "https://books.toscrape.com/"));
    out.put("after_close", Map.of("error", after.error(), "text", firstLine(after.text())));
    var account = JsonParser.parseString(call(client, log, "account", Map.of()).text()).getAsJsonObject();
    out.put("live_sessions_after", account.get("live_sessions").getAsInt());
    client.closeGracefully(); // ends the MCP session (DELETE)
    out.put("calls", log);
    return out;
  }

  // ---- 2. A new MCP session for every tool call, as ChatGPT does.
  static Map<String, Object> perCallSessions() {
    var log = new ArrayList<Map<String, Object>>();
    var sessions = new ArrayList<String>();
    java.util.function.BiFunction<String, Map<String, Object>, String> once = (name, args) -> {
      long t = System.currentTimeMillis();
      var seen = new HashSet<String>();
      var client = connect(seen);
      var text = (name.equals("navigate") ? navigate(client, log, (String) args.get("url")) : call(client, log, name, args)).text();
      sessions.add(seen.isEmpty() ? null : seen.iterator().next());
      client.close(); // no graceful shutdown: the client just goes away
      log.get(log.size() - 1).put("with_initialize_ms", System.currentTimeMillis() - t);
      return text;
    };
    var launched = once.apply("launch_browser", launchArgs());
    var sm = SESSION.matcher(launched);
    String sid = sm.find() ? sm.group(1) : "";
    var nav = once.apply("navigate", Map.of("url", "https://books.toscrape.com/catalogue/page-2.html"));
    var where = once.apply("evaluate", Map.of("expression", "({ url: location.href, books: document.querySelectorAll(\"article.product_pod\").length })"));
    var closed = once.apply("close_browser", Map.of());
    var sees = JsonParser.parseString(where).getAsJsonObject();
    var out = new LinkedHashMap<String, Object>();
    out.put("mcp_sessions", sessions.size());
    out.put("distinct_mcp_sessions", sessions.stream().filter(Objects::nonNull).distinct().count());
    out.put("browser_session_id", sid);
    out.put("navigate", firstLine(nav));
    out.put("later_session_sees", Map.of("url", sees.get("url").getAsString(), "books", sees.get("books").getAsInt()));
    out.put("closed_same_browser", closed.startsWith("Session " + sid + " ended"));
    out.put("calls", log);
    return out;
  }

  public static void main(String[] args) {
    var one = oneSession();
    var perCall = perCallSessions();
    var result = new LinkedHashMap<String, Object>();
    result.put("language", "java");
    result.put("one_session", one);
    result.put("per_call", perCall);
    result.put("measured_at", java.time.LocalDate.now(java.time.ZoneOffset.UTC).toString());
    System.out.println(new GsonBuilder().setPrettyPrinting().disableHtmlEscaping().create().toJson(result));
    System.exit(0); // the SDK's HTTP client keeps non-daemon threads
  }
}
