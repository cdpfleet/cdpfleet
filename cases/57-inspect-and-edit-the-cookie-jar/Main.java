// Maven: com.microsoft.playwright:playwright:1.60.0, com.google.code.gson:gson:2.11.0
// Run with PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1.
// env: CDPFLEET_API_KEY, PROXY_URL (your own proxy; a few KB of traffic)
import com.google.gson.*;
import com.microsoft.playwright.*;
import com.microsoft.playwright.options.Cookie;
import java.net.URI;
import java.net.http.*;
import java.util.*;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");
  static final String PROXY = System.getenv("PROXY_URL");
  static final String SITE = "https://www.httpbin.org";
  static final Gson GSON = new GsonBuilder().setPrettyPrinting().disableHtmlEscaping().serializeNulls().create();
  static final HttpClient HTTP = HttpClient.newHttpClient();

  // 503 means the fleet is momentarily busy: wait Retry-After and try again.
  static JsonObject launch(String engine, JsonObject body) throws Exception {
    for (int attempt = 1; ; attempt++) {
      HttpResponse<String> res = HTTP.send(HttpRequest.newBuilder(URI.create("https://starter.cdpfleet.com/" + engine + "/session"))
          .header("x-api-key", KEY).header("content-type", "application/json")
          .POST(HttpRequest.BodyPublishers.ofString(body.toString())).build(), HttpResponse.BodyHandlers.ofString());
      if (res.statusCode() == 200) return JsonParser.parseString(res.body()).getAsJsonObject();
      if (res.statusCode() != 503 || attempt == 5) throw new RuntimeException("launch " + res.statusCode() + " " + res.body());
      Thread.sleep(Long.parseLong(res.headers().firstValue("retry-after").orElse("5")) * 1000);
    }
  }

  static Browser open(Playwright pw, String engine) throws Exception {
    JsonObject body = new JsonObject();
    body.addProperty("proxy", PROXY);
    body.addProperty("headless", true);
    body.addProperty("record", false);
    JsonObject session = launch(engine, body);
    BrowserType type = switch (engine) { case "firefox" -> pw.firefox(); case "webkit" -> pw.webkit(); default -> pw.chromium(); };
    return type.connect(session.get("wsUrl").getAsString(), new BrowserType.ConnectOptions().setHeaders(Map.of("x-api-key", KEY)));
  }

  // httpbin answers 502/503 now and then: retry a page that isn't JSON.
  static JsonObject json(Page page, String url) throws Exception {
    for (int attempt = 1; ; attempt++) {
      page.navigate(url, new Page.NavigateOptions().setTimeout(45000));
      try {
        return JsonParser.parseString(page.textContent("body")).getAsJsonObject();
      } catch (JsonParseException | IllegalStateException e) {
        if (attempt == 4) throw e;
        Thread.sleep(3000);
      }
    }
  }

  static JsonArray names(List<Cookie> cookies) {
    JsonArray out = new JsonArray();
    cookies.stream().map(c -> c.name).sorted().forEach(out::add);
    return out;
  }

  static Cookie find(List<Cookie> cookies, String name) {
    return cookies.stream().filter(c -> c.name.equals(name)).findFirst().orElseThrow();
  }

  static boolean has(JsonArray names, String name) {
    return names.contains(new JsonPrimitive(name));
  }

  public static void main(String[] args) throws Exception {
    JsonObject jar = new JsonObject();
    JsonArray psl = new JsonArray();
    try (Playwright pw = Playwright.create()) {
      // Part 1: inspect, edit, clear and carry the jar (Chromium).
      String saved;
      Browser browser = open(pw, "chromium");
      try {
        BrowserContext context = browser.newContext();
        Page page = context.newPage();
        // The site sets two cookies (no expiry: session cookies); another site sets one more.
        json(page, SITE + "/cookies/set?session_token=abc123&theme=dark");
        context.addCookies(List.of(new Cookie("other_site", "1").setUrl("https://example.com")));
        jar.add("all_cookies", names(context.cookies()));
        List<Cookie> mine = context.cookies(SITE); // only the cookies that would be sent to this URL
        jar.add("for_site", names(mine));
        jar.addProperty("session_cookie_expires", find(mine, "session_token").expires.longValue()); // -1 = session cookie

        // Edit: addCookies with the same name, domain and path replaces the cookie.
        Cookie token = find(mine, "session_token");
        token.expires = (double) (System.currentTimeMillis() / 1000 + 7 * 86400);
        Cookie theme = find(mine, "theme");
        theme.value = "light";
        context.addCookies(List.of(token, theme));
        double expires = find(context.cookies(SITE), "session_token").expires;
        jar.addProperty("token_expires_in_days", Math.round((expires - System.currentTimeMillis() / 1000.0) / 86400));
        jar.add("server_sees_after_edit", json(page, SITE + "/cookies").get("cookies"));

        // Clear one cookie by name (other filters: domain, path).
        context.clearCookies(new BrowserContext.ClearCookiesOptions().setName("theme"));
        jar.add("after_clear_by_name", names(context.cookies()));

        // Carry the login as plain JSON: just the site's cookies.
        saved = new Gson().toJson(context.cookies(SITE));
        jar.addProperty("saved_json_bytes", saved.length());
      } finally {
        browser.close();
      }

      browser = open(pw, "chromium"); // a new session on another browser
      try {
        BrowserContext context = browser.newContext();
        jar.add("new_session_before", names(context.cookies()));
        context.addCookies(Arrays.asList(new Gson().fromJson(saved, Cookie[].class)));
        Page page = context.newPage();
        jar.add("new_session_server_sees", json(page, SITE + "/cookies").get("cookies"));
      } finally {
        browser.close();
      }

      // Part 2: the same three ways to set a cookie, on three hosts, in three engines.
      // httpbin.org and postman-echo.com are on the Public Suffix List; www.httpbin.org isn't.
      for (String engine : List.of("chromium", "firefox", "webkit")) {
        browser = open(pw, engine);
        try {
          for (String host : List.of("httpbin.org", "www.httpbin.org", "postman-echo.com")) {
            BrowserContext context = browser.newContext();
            Page page = context.newPage();
            String url = "https://" + host;
            context.addCookies(List.of(new Cookie("via_add_cookies", "1").setUrl(url)));
            json(page, url + "/cookies/set?via_set_cookie=1"); // Set-Cookie, then a redirect to /cookies
            page.evaluate("() => { document.cookie = 'via_document_cookie=1; path=/; max-age=3600'; }");
            JsonArray kept = names(context.cookies(url));
            JsonArray server = new JsonArray();
            json(page, url + "/cookies").getAsJsonObject("cookies").keySet().stream().sorted().forEach(server::add);
            JsonObject r = new JsonObject();
            r.addProperty("engine", engine);
            r.addProperty("host", host);
            r.addProperty("add_cookies", has(kept, "via_add_cookies"));
            r.addProperty("set_cookie", has(kept, "via_set_cookie"));
            r.addProperty("document_cookie", has(kept, "via_document_cookie"));
            r.add("server_sees", server);
            psl.add(r);
            context.close();
          }
        } finally {
          browser.close();
        }
      }
    }
    JsonObject out = new JsonObject();
    out.add("jar", jar);
    out.add("psl", psl);
    System.out.println(GSON.toJson(out));
  }
}
