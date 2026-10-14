// Maven: com.microsoft.playwright:playwright:1.60.0, com.google.code.gson:gson:2.11.0
// Run with PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1.
// env: CDPFLEET_API_KEY, PROXY_URL
import com.google.gson.*;
import com.microsoft.playwright.*;
import com.microsoft.playwright.options.FilePayload;
import java.net.URI;
import java.net.http.*;
import java.nio.charset.StandardCharsets;
import java.util.*;
import java.util.regex.*;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");
  static final Gson GSON = new GsonBuilder().setPrettyPrinting().disableHtmlEscaping().create();

  record Setup(String label, String engine, String family, boolean headless) {}
  static final Setup[] SETUPS = {
    new Setup("Chromium, headless", "chromium", "chromium", true),
    new Setup("Chrome, headful", "chrome", "chromium", false),
    new Setup("Firefox, headful", "firefox", "firefox", false),
    new Setup("WebKit, headful", "webkit", "webkit", false),
  };

  // A made-up https origin served by Playwright's router: a secure context (the clipboard and
  // file APIs need one) with no network traffic. The page has a file input and a button that
  // opens the File System Access picker; both report what they received in data attributes.
  static final String PAGE = "https://app.example.test/";
  static final String HTML = """
      <html><body>
      <input type="file" id="f" onchange="document.body.dataset.input = this.files[0].name + ':' + this.files[0].size">
      <button id="b" onclick="showOpenFilePicker().then(async ([h]) => { const f = await h.getFile(); document.body.dataset.picked = f.name + ':' + f.size; }).catch((e) => { document.body.dataset.picked = 'error: ' + e.name; })">open</button>
      </body></html>
      """;

  // Clipboard round trip inside the page; a permission prompt nobody answers would hang, so 3 s cap.
  static final String CLIPBOARD = """
      (async () => {
        const cap = (p) => Promise.race([p, new Promise((_, rej) => setTimeout(() => rej(new Error('timed out (prompt?)')), 3000))]);
        const out = {};
        try { await cap(navigator.clipboard.writeText('copied-' + Date.now())); out.write = 'ok'; } catch (e) { out.write = e.name === 'Error' ? e.message : e.name; }
        try { const t = await cap(navigator.clipboard.readText()); out.read = t.startsWith('copied-') ? 'ok' : 'empty'; } catch (e) { out.read = e.name === 'Error' ? e.message : e.name; }
        return out;
      })()""";

  // The origin-private file system: a sandboxed folder per site, no picker, no permission.
  static final String OPFS = """
      (async () => {
        try {
          const root = await navigator.storage.getDirectory();
          const fh = await root.getFileHandle('note.txt', { create: true });
          const w = await fh.createWritable(); await w.write('hello opfs'); await w.close();
          return (await (await fh.getFile()).text()) === 'hello opfs' ? 'ok' : 'mismatch';
        } catch (e) { return 'no (' + e.name + ')'; }
      })()""";

  // Stands in for the native picker: returns a handle to a file the script provides.
  static final String PICKER_STUB = """
      window.showOpenFilePicker = async () => [{
        kind: 'file', name: 'report.csv', getFile: async () => new File(['id,total\\n1,42\\n'], 'report.csv', { type: 'text/csv' }),
      }];""";

  // 503 means the fleet is momentarily busy: wait Retry-After and try again.
  static JsonObject launch(String engine, JsonObject body) throws Exception {
    for (int attempt = 1; ; attempt++) {
      HttpResponse<String> res = HttpClient.newHttpClient().send(HttpRequest.newBuilder(URI.create("https://starter.cdpfleet.com/" + engine + "/session"))
          .header("x-api-key", KEY).header("content-type", "application/json")
          .POST(HttpRequest.BodyPublishers.ofString(body.toString())).build(), HttpResponse.BodyHandlers.ofString());
      if (res.statusCode() == 200) return JsonParser.parseString(res.body()).getAsJsonObject();
      if (res.statusCode() != 503 || attempt == 5) throw new RuntimeException("launch " + engine + " " + res.statusCode() + " " + res.body());
      Thread.sleep(Long.parseLong(res.headers().firstValue("retry-after").orElse("5")) * 1000);
    }
  }

  static void serve(BrowserContext context) {
    context.route(PAGE + "**", route -> route.fulfill(new Route.FulfillOptions().setContentType("text/html").setBody(HTML)));
  }

  static Page freshPage(Browser browser, String initScript) {
    BrowserContext context = browser.newContext();
    serve(context);
    if (initScript != null) context.addInitScript(initScript);
    Page page = context.newPage();
    page.navigate(PAGE);
    return page;
  }

  static FileChooser chooserAfterClick(Page page, String selector) {
    try {
      return page.waitForFileChooser(new Page.WaitForFileChooserOptions().setTimeout(3000), () -> page.click(selector));
    } catch (TimeoutError e) {
      return null;
    }
  }

  @SuppressWarnings("unchecked")
  static JsonObject one(Playwright pw, Setup s) throws Exception {
    JsonObject body = new JsonObject();
    body.addProperty("proxy", System.getenv("PROXY_URL"));
    body.addProperty("headless", s.headless());
    JsonObject session = launch(s.engine(), body);
    BrowserType type = switch (s.family()) { case "firefox" -> pw.firefox(); case "webkit" -> pw.webkit(); default -> pw.chromium(); };
    Browser browser = type.connect(session.get("wsUrl").getAsString(), new BrowserType.ConnectOptions().setHeaders(Map.of("x-api-key", KEY)));
    JsonObject out = new JsonObject();
    out.addProperty("label", s.label());
    try {
      // 1. Clipboard with no permissions granted.
      Page page = freshPage(browser, null);
      Map<String, Object> d = (Map<String, Object>) page.evaluate(CLIPBOARD);
      out.addProperty("clipboard_write_default", (String) d.get("write"));
      out.addProperty("clipboard_read_default", (String) d.get("read"));

      // 2. Files in the same page: <input type=file>, the File System Access picker, the private file system.
      FileChooser fc = chooserAfterClick(page, "#f");
      if (fc != null) fc.setFiles(new FilePayload("report.csv", "text/csv", "id,total\n1,42\n".getBytes(StandardCharsets.UTF_8)));
      out.addProperty("input_set_files", fc != null ? (String) page.evaluate("() => document.body.dataset.input || 'nothing'") : "no filechooser event");
      boolean hasPicker = (Boolean) page.evaluate("() => typeof window.showOpenFilePicker === 'function'");
      out.addProperty("show_open_file_picker", hasPicker);
      out.addProperty("picker_filechooser_event", hasPicker && chooserAfterClick(page, "#b") != null);
      out.addProperty("private_file_system", (String) page.evaluate(OPFS));
      page.context().close();

      // 3. The picker stubbed with an init script: the page's own code receives our file.
      page = freshPage(browser, PICKER_STUB);
      page.click("#b");
      page.waitForFunction("() => document.body.dataset.picked", null, new Page.WaitForFunctionOptions().setTimeout(5000));
      out.addProperty("picker_stubbed", (String) page.evaluate("() => document.body.dataset.picked"));
      page.context().close();

      // 4. Clipboard with Chromium's permission names granted. Some engines reject the names —
      // at grantPermissions or when the next page opens.
      BrowserContext context = browser.newContext();
      serve(context);
      try {
        context.grantPermissions(List.of("clipboard-read", "clipboard-write"), new BrowserContext.GrantPermissionsOptions().setOrigin(PAGE));
        page = context.newPage();
        page.navigate(PAGE);
        out.addProperty("grant_clipboard", "accepted");
        Map<String, Object> g = (Map<String, Object>) page.evaluate(CLIPBOARD);
        out.addProperty("clipboard_write_granted", (String) g.get("write"));
        out.addProperty("clipboard_read_granted", (String) g.get("read"));
      } catch (PlaywrightException err) {
        // Java wraps the message: Error { message='Unknown permission: …' … }; drop any "api.call: " prefix.
        Matcher m = Pattern.compile("message='([^'\\n]*)").matcher(err.getMessage());
        String msg = (m.find() ? m.group(1) : err.getMessage().split("\n")[0]).replaceFirst("^[A-Za-z]+\\.[A-Za-z]+: ", "");
        out.addProperty("grant_clipboard", msg.substring(0, Math.min(80, msg.length())));
      }
    } finally {
      browser.close();
    }
    return out;
  }

  public static void main(String[] args) throws Exception {
    JsonArray results = new JsonArray();
    try (Playwright pw = Playwright.create()) {
      for (Setup s : SETUPS) results.add(one(pw, s));
    }
    JsonObject result = new JsonObject();
    result.add("results", results);
    System.out.println(GSON.toJson(result));
  }
}
