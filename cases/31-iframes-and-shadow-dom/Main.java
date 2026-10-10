// Maven: com.microsoft.playwright:playwright:1.60.0, com.google.code.gson:gson:2.11.0
// Run with PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1. env: CDPFLEET_API_KEY, PROXY_URL
import com.google.gson.*;
import com.microsoft.playwright.*;
import java.net.URI;
import java.net.http.*;
import java.util.*;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");
  static final Gson GSON = new GsonBuilder().serializeNulls().create();

  // Content a plain document.querySelector can't see: a same-origin iframe, a cross-origin
  // iframe, an open shadow root (with another one nested inside) and a closed shadow root.
  static final String HTML = """
      <!doctype html><title>Hidden content</title>
      <h1>Main document</h1>
      <iframe id="same" srcdoc="<p id='inner'>same-origin iframe text</p>"></iframe>
      <iframe id="cross" src="https://httpbin.org/html"></iframe>
      <open-card></open-card>
      <closed-card></closed-card>
      <script>
      customElements.define('open-card', class extends HTMLElement {
        connectedCallback() {
          const root = this.attachShadow({ mode: 'open' });
          root.innerHTML = '<p class="msg">open shadow text</p><nested-badge></nested-badge>';
        }
      });
      customElements.define('nested-badge', class extends HTMLElement {
        connectedCallback() { this.attachShadow({ mode: 'open' }).innerHTML = '<span class="badge">nested shadow text</span>'; }
      });
      customElements.define('closed-card', class extends HTMLElement {
        connectedCallback() { this.attachShadow({ mode: 'closed' }).innerHTML = '<p class="secret">closed shadow text</p>'; }
      });
      </script>""";

  static String qs(Page page, String sel) {
    return (String) page.evaluate("(s) => document.querySelector(s)?.textContent ?? null", sel);
  }

  static String pw(Locator loc) {
    return loc.count() > 0 ? loc.first().textContent() : null;
  }

  static JsonObject row(String target, String querySelector, String playwright, String how) {
    JsonObject r = new JsonObject();
    r.addProperty("target", target);
    r.addProperty("querySelector", querySelector);
    r.addProperty("playwright", playwright);
    r.addProperty("how", how);
    return r;
  }

  public static void main(String[] args) throws Exception {
    String body = "{\"proxy\": " + GSON.toJson(System.getenv("PROXY_URL")) + ", \"headless\": \"new\"}";
    HttpResponse<String> res = HttpClient.newHttpClient().send(HttpRequest.newBuilder(URI.create("https://starter.cdpfleet.com/chromium/session"))
        .header("x-api-key", KEY).header("content-type", "application/json")
        .POST(HttpRequest.BodyPublishers.ofString(body)).build(), HttpResponse.BodyHandlers.ofString());
    if (res.statusCode() != 200) throw new RuntimeException("launch: " + res.statusCode() + " " + res.body());
    String wsUrl = JsonParser.parseString(res.body()).getAsJsonObject().get("wsUrl").getAsString();

    try (Playwright playwright = Playwright.create()) {
      Browser browser = playwright.chromium().connect(wsUrl, new BrowserType.ConnectOptions().setHeaders(Map.of("x-api-key", KEY)));
      try {
        Page page = browser.newPage();
        page.setContent(HTML);
        page.frameLocator("#cross").locator("h1").waitFor(new Locator.WaitForOptions().setTimeout(60000));

        JsonArray rows = new JsonArray();
        rows.add(row("same-origin iframe", qs(page, "#inner"), pw(page.frameLocator("#same").locator("#inner")), "page.frameLocator('#same').locator('#inner')"));
        rows.add(row("cross-origin iframe", qs(page, "h1 + div p"), pw(page.frameLocator("#cross").locator("h1")), "page.frameLocator('#cross').locator('h1')"));
        rows.add(row("open shadow root", qs(page, ".msg"), pw(page.locator(".msg")), "page.locator('.msg') — CSS pierces open shadow roots"));
        rows.add(row("nested open shadow root", qs(page, ".badge"), pw(page.locator(".badge")), "page.locator('.badge') — any depth"));
        rows.add(row("closed shadow root", qs(page, ".secret"), pw(page.locator(".secret")), "not reachable from page scripts or locators"));

        // The cross-origin frame is a separate document: its URL and title come from the frame object.
        Frame cross = page.frames().stream().filter(f -> f.url().startsWith("https://httpbin.org")).findFirst().orElse(null);
        JsonObject crossOut = new JsonObject();
        crossOut.addProperty("url", cross != null ? cross.url() : null);
        crossOut.addProperty("title", cross != null ? cross.title() : null);

        JsonObject out = new JsonObject();
        out.addProperty("frames", page.frames().size());
        out.add("cross_origin_frame", crossOut);
        out.add("rows", rows);

        System.out.println(new GsonBuilder().serializeNulls().setPrettyPrinting().disableHtmlEscaping().create().toJson(out));
      } finally {
        browser.close();
      }
    }
  }
}
