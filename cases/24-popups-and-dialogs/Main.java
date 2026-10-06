// Maven: com.microsoft.playwright:playwright:1.60.0, com.google.code.gson:gson:2.11.0
// Run with PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1. env: CDPFLEET_API_KEY, PROXY_URL
import com.google.gson.*;
import com.microsoft.playwright.*;
import com.microsoft.playwright.options.LoadState;
import java.net.URI;
import java.net.http.*;
import java.util.*;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");
  static final Gson GSON = new GsonBuilder().setPrettyPrinting().disableHtmlEscaping().serializeNulls().create();

  // A page with everything that interrupts a script: a new-tab link, window.open, and the
  // three blocking dialogs. Served from the browser itself, so the case needs no third party.
  static final String HTML = "<!doctype html><title>Interruptions</title>\n"
      + "<a id=\"blank\" href=\"https://example.com/\" target=\"_blank\">open in a new tab</a>\n"
      + "<button id=\"open\" onclick=\"window.open('https://example.com/?popup', 'pop', 'width=480,height=320')\">window.open</button>\n"
      + "<button id=\"alert\" onclick=\"alert('Saved!')\">alert</button>\n"
      + "<button id=\"confirm\" onclick=\"document.body.dataset.confirm = String(confirm('Delete 3 items?'))\">confirm</button>\n"
      + "<button id=\"prompt\" onclick=\"document.body.dataset.prompt = String(prompt('Your name?', 'anonymous'))\">prompt</button>";

  static JsonObject row(String event, String what, String handledWith, int pagesOpen, Boolean opener) {
    JsonObject row = new JsonObject();
    row.addProperty("event", event);
    row.addProperty("what_happened", what);
    row.addProperty("handled_with", handledWith);
    row.addProperty("pages_open", pagesOpen);
    if (opener == null) row.add("opener_is_main_page", JsonNull.INSTANCE); else row.addProperty("opener_is_main_page", opener);
    return row;
  }

  public static void main(String[] args) throws Exception {
    String body = "{\"proxy\": " + GSON.toJson(System.getenv("PROXY_URL")) + ", \"headless\": \"new\"}";
    HttpResponse<String> res = HttpClient.newHttpClient().send(HttpRequest.newBuilder(URI.create("https://starter.cdpfleet.com/chromium/session"))
        .header("x-api-key", KEY).header("content-type", "application/json")
        .POST(HttpRequest.BodyPublishers.ofString(body)).build(), HttpResponse.BodyHandlers.ofString());
    if (res.statusCode() != 200) throw new RuntimeException("launch " + res.statusCode() + " " + res.body());
    JsonObject session = JsonParser.parseString(res.body()).getAsJsonObject();

    try (Playwright playwright = Playwright.create()) {
      Browser browser = playwright.chromium().connect(session.get("wsUrl").getAsString(),
          new BrowserType.ConnectOptions().setHeaders(Map.of("x-api-key", KEY)));
      try {
        BrowserContext context = browser.newContext();
        Page page = context.newPage();
        page.setContent(HTML);
        JsonArray out = new JsonArray();

        // New pages: listen on the context BEFORE the click, then wait for the popup to load.
        for (String[] target : new String[][] {{"link with target=_blank", "#blank"}, {"window.open()", "#open"}}) {
          Page popup = context.waitForPage(() -> page.click(target[1]));
          popup.waitForLoadState(LoadState.LOAD, new Page.WaitForLoadStateOptions().setTimeout(60000));
          out.add(row(target[0], "new page: " + popup.url() + " — \"" + popup.title() + "\"",
              "context.waitForEvent(\"page\") + popup.waitForLoadState()", context.pages().size(), popup.opener() == page));
          popup.close();
        }

        // Dialogs: without a handler Playwright dismisses them (confirm → false, prompt → null).
        page.click("#confirm");
        out.add(row("confirm() with no dialog handler", "page saw confirm() return " + page.evaluate("() => document.body.dataset.confirm"),
            "nothing — auto-dismissed", context.pages().size(), null));

        // With a handler you decide: accept, dismiss, or type an answer.
        List<String> seen = new ArrayList<>();
        page.onDialog(d -> {
          seen.add(d.type() + ": \"" + d.message() + "\"");
          if (d.type().equals("prompt")) d.accept("Ada Lovelace"); else d.accept();
        });
        page.click("#alert");
        page.click("#confirm");
        page.click("#prompt");
        JsonObject results = GSON.toJsonTree(page.evaluate("() => ({ confirm: document.body.dataset.confirm, prompt: document.body.dataset.prompt })")).getAsJsonObject();
        out.add(row("alert()", seen.get(0), "dialog.accept()", context.pages().size(), null));
        out.add(row("confirm() with a handler", seen.get(1) + " → page saw " + results.get("confirm").getAsString(), "dialog.accept()", context.pages().size(), null));
        out.add(row("prompt()", seen.get(2) + " → page saw \"" + results.get("prompt").getAsString() + "\"", "dialog.accept(\"Ada Lovelace\")", context.pages().size(), null));

        System.out.println(GSON.toJson(out));
      } finally {
        browser.close();
      }
    }
  }
}
