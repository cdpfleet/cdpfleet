// Maven: com.microsoft.playwright:playwright:1.60.0, com.google.code.gson:gson:2.11.0
// Run with PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1.
// env: CDPFLEET_API_KEY, PROXY_URL
import com.google.gson.*;
import com.microsoft.playwright.*;
import java.net.URI;
import java.net.http.*;
import java.nio.charset.StandardCharsets;
import java.util.*;
import java.util.concurrent.atomic.AtomicInteger;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");
  static final String SITE = "https://practicesoftwaretesting.com"; // Toolshop: a public demo shop built for test automation
  static final String API = "https://api.practicesoftwaretesting.com";
  static final String EMAIL = "customer@practicesoftwaretesting.com", PASSWORD = "welcome01"; // the site's published demo account

  // Fetches every page of the API from inside the page with the token: one by one, then all at once.
  static final String FETCH_ALL = String.join("\n",
      "async ({ api, token }) => {",
      "  const get = (n) => fetch(`${api}/invoices?page=${n}`, { headers: { authorization: `Bearer ${token}` } }).then(async (r) => ({ status: r.status, text: await r.text() }));",
      "  const meta = JSON.parse((await get(1)).text);",
      "  const numbers = Array.from({ length: meta.last_page }, (_, i) => i + 1);",
      "  let t = performance.now();",
      "  const pages = [];",
      "  for (const n of numbers) pages.push(await get(n));",
      "  const sequential = (performance.now() - t) / 1000;",
      "  t = performance.now();",
      "  await Promise.all(numbers.map(get));",
      "  const parallel = (performance.now() - t) / 1000;",
      "  const noToken = await fetch(`${api}/invoices?page=1`).then((r) => r.status);",
      "  const items = pages.flatMap((p) => JSON.parse(p.text).data);",
      "  return JSON.stringify({",
      "    sequential, parallel, total: meta.total, perPage: meta.per_page, lastPage: meta.last_page,",
      "    requests: pages.length, bytes: pages.reduce((s, p) => s + p.text.length, 0), statuses: [...new Set(pages.map((p) => p.status))],",
      "    invoices: items.map((i) => i.invoice_number), fields: Object.keys(items[0]).length, noToken,",
      "  });",
      "}");

  // 503 means the fleet is momentarily busy: wait Retry-After and try again.
  static JsonObject launch(JsonObject body) throws Exception {
    for (int attempt = 1; ; attempt++) {
      HttpResponse<String> res = HttpClient.newHttpClient().send(HttpRequest.newBuilder(URI.create("https://starter.cdpfleet.com/chromium/session"))
          .header("x-api-key", KEY).header("content-type", "application/json")
          .POST(HttpRequest.BodyPublishers.ofString(body.toString())).build(), HttpResponse.BodyHandlers.ofString());
      if (res.statusCode() == 200) return JsonParser.parseString(res.body()).getAsJsonObject();
      if (res.statusCode() != 503 || attempt == 5) throw new RuntimeException("launch " + res.statusCode() + " " + res.body());
      Thread.sleep(Long.parseLong(res.headers().firstValue("retry-after").orElse("5")) * 1000);
    }
  }

  @SuppressWarnings("unchecked")
  static List<String> readTable(Page page) {
    return (List<String>) page.evalOnSelectorAll("table tbody tr td:first-child", "tds => tds.map((td) => td.textContent.trim())");
  }

  public static void main(String[] args) throws Exception {
    JsonObject body = new JsonObject();
    body.addProperty("proxy", System.getenv("PROXY_URL"));
    body.addProperty("headless", "new");
    body.addProperty("record", false);
    String wsUrl = launch(body).get("wsUrl").getAsString();
    try (Playwright playwright = Playwright.create()) {
      Browser browser = playwright.chromium().connect(wsUrl, new BrowserType.ConnectOptions().setHeaders(Map.of("x-api-key", KEY)));
      try {
        Page page = browser.newPage();
        // The app can take 30 s to render through a rotating residential exit: wait up to 90 s.
        page.setDefaultTimeout(90000);

        // 1. Log in like a person; the site stores its token and sends it with every API call.
        page.navigate(SITE + "/auth/login");
        page.fill("#email", EMAIL);
        page.fill("#password", PASSWORD);
        page.click("[type=submit]");
        page.waitForURL("**/account");

        // 2. Find the token twice: in localStorage, and in the header of the page's own API request.
        String stored = (String) page.evaluate("() => localStorage.getItem('auth-token')");
        AtomicInteger uiCalls = new AtomicInteger();
        java.util.function.Consumer<Request> countCall = r -> { if (r.url().startsWith(API + "/invoices")) uiCalls.incrementAndGet(); };
        page.onRequest(countCall);
        Request firstCall = page.waitForRequest(r -> r.url().startsWith(API + "/invoices"), () -> page.navigate(SITE + "/account/invoices"));
        String header = firstCall.allHeaders().getOrDefault("authorization", "");
        JsonObject claims = JsonParser.parseString(new String(Base64.getUrlDecoder().decode(stored.split("\\.")[1]), StandardCharsets.UTF_8)).getAsJsonObject();

        // 3. The page's way: click through the pager, reading the table each time.
        page.waitForSelector("table tbody tr");
        long t0 = System.nanoTime();
        List<String> viaUi = new ArrayList<>(readTable(page));
        int lastPage = Integer.parseInt(page.locator("button[aria-label^=\"Page-\"]").last().innerText().trim());
        for (int n = 2; n <= lastPage; n++) {
          String want = API + "/invoices?page=" + n;
          page.waitForResponse(r -> r.url().equals(want), () -> page.click("[data-test=\"pagination-next\"]"));
          page.waitForFunction("first => document.querySelector('table tbody tr td')?.textContent.trim() !== first", viaUi.get(viaUi.size() - 15));
          viaUi.addAll(readTable(page));
        }
        double uiSeconds = (System.nanoTime() - t0) / 1e9;
        page.offRequest(countCall);
        int columns = page.locator("table thead th").count();

        // 4. The API's way: the same token on fetch() from inside the page — the browser makes the
        //    requests, through the session's proxy.
        JsonObject viaApi = JsonParser.parseString((String) page.evaluate(FETCH_ALL, Map.of("api", API, "token", stored))).getAsJsonObject();
        List<String> apiInvoices = new ArrayList<>();
        for (JsonElement e : viaApi.getAsJsonArray("invoices")) apiInvoices.add(e.getAsString());
        List<String> a = new ArrayList<>(viaUi), b = new ArrayList<>(apiInvoices);
        Collections.sort(a);
        Collections.sort(b);

        JsonObject token = new JsonObject();
        token.addProperty("in_local_storage", stored != null ? "auth-token" : null);
        token.addProperty("header_matches", header.equals("Bearer " + stored));
        token.addProperty("lifetime_seconds", claims.get("exp").getAsLong() - claims.get("iat").getAsLong());
        token.addProperty("seconds_left_at_end", claims.get("exp").getAsLong() - System.currentTimeMillis() / 1000);
        JsonObject api = new JsonObject();
        api.add("total", viaApi.get("total"));
        api.add("per_page", viaApi.get("perPage"));
        api.add("last_page", viaApi.get("lastPage"));
        api.add("without_token", viaApi.get("noToken"));
        JsonObject ui = new JsonObject();
        ui.addProperty("pages", lastPage);
        ui.addProperty("invoices", viaUi.size());
        ui.addProperty("invoice_calls", uiCalls.get());
        ui.addProperty("columns", columns);
        ui.addProperty("seconds", uiSeconds);
        JsonObject direct = new JsonObject();
        direct.add("requests", viaApi.get("requests"));
        direct.add("statuses", viaApi.get("statuses"));
        direct.addProperty("invoices", apiInvoices.size());
        direct.add("fields_per_invoice", viaApi.get("fields"));
        direct.add("json_bytes", viaApi.get("bytes"));
        direct.add("seconds_one_by_one", viaApi.get("sequential"));
        direct.add("seconds_all_at_once", viaApi.get("parallel"));
        JsonObject out = new JsonObject();
        out.add("token", token);
        out.add("api", api);
        out.add("ui", ui);
        out.add("direct", direct);
        out.addProperty("same_invoices", a.equals(b));
        Set<String> missing = new HashSet<>(viaUi);
        missing.removeAll(apiInvoices);
        out.addProperty("ui_missing_from_api", missing.size());
        System.out.println(out);
      } finally {
        browser.close();
      }
    }
  }
}
