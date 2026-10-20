// Maven: com.microsoft.playwright:playwright:1.60.0, com.google.code.gson:gson:2.11.0
// Run with PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1. env: CDPFLEET_API_KEY, PROXY_URL
import com.google.gson.*;
import com.microsoft.playwright.*;
import com.microsoft.playwright.options.*;
import java.net.URI;
import java.net.http.*;
import java.time.*;
import java.util.*;
import java.util.regex.*;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");
  // Public pages, each loaded once: {url, expected vendor, demo name or null}. Demo pages are the
  // protection vendors' own test pages and are reported by name; for the rest only the counts per
  // vendor are reported. The expected vendor is what a first survey (2026-10-19) found.
  static final String[][] PAGES = {
      {"https://nowsecure.nl/", "turnstile", "nowsecure.nl (Turnstile test page)"},
      {"https://www.scrapingcourse.com/cloudflare-challenge", "cloudflare", "scrapingcourse.com Cloudflare challenge"},
      {"https://peet.ws/turnstile-test/non-interactive.html", "turnstile", "peet.ws Turnstile test (non-interactive)"},
      {"https://recaptcha-demo.appspot.com/recaptcha-v3-request-scores.php", "recaptcha", "Google reCAPTCHA v3 demo"},
      {"https://accounts.hcaptcha.com/demo", "hcaptcha", "hCaptcha demo"},
      {"https://www.crunchbase.com/", "cloudflare", null},
      {"https://discord.com/", "cloudflare", null},
      {"https://www.glassdoor.com/", "cloudflare", null},
      {"https://www.tripadvisor.com/", "datadome", null},
      {"https://www.etsy.com/", "datadome", null},
      {"https://soundcloud.com/", "datadome", null},
      {"https://www.homedepot.com/", "akamai", null},
      {"https://www.usps.com/", "akamai", null},
      {"https://www.delta.com/", "akamai", null},
      {"https://www.zillow.com/", "human", null},
      {"https://www.walmart.com/", "human", null},
      {"https://www.realestate.com.au/", "kasada", null},
      {"https://www.ticketmaster.com/", "kasada", null},
      {"https://www.amazon.com/", "awswaf", null},
      {"https://www.booking.com/", "awswaf", null}};
  static final List<String> VENDORS = List.of("cloudflare", "turnstile", "datadome", "akamai", "human", "kasada", "imperva", "awswaf", "recaptcha", "hcaptcha");

  interface Test { boolean check(Map<String, String> h, List<String> cookies, List<String> urls); }
  record Signal(String vendor, String label, Test test) {}

  // A lenient host match: URI.create() rejects the unencoded characters some pages put in URLs.
  static final Pattern HOST = Pattern.compile("^[a-z]+://([^/:?#]+)");
  static String host(String u) { Matcher m = HOST.matcher(u); return m.find() ? m.group(1).toLowerCase() : ""; }
  static boolean anyHost(List<String> urls, String re) { Pattern p = Pattern.compile(re); return urls.stream().anyMatch(u -> p.matcher(host(u)).find()); }

  // Each vendor leaves response headers, cookies or requests to its own hosts.
  static final List<Signal> SIGNALS = List.of(
      new Signal("cloudflare", "cf-ray header", (h, c, u) -> h.containsKey("cf-ray")),
      new Signal("cloudflare", "__cf_bm / cf_clearance cookie", (h, c, u) -> c.stream().anyMatch(x -> List.of("__cf_bm", "cf_clearance", "__cflb").contains(x))),
      new Signal("cloudflare", "/cdn-cgi/challenge-platform request", (h, c, u) -> u.stream().anyMatch(x -> x.contains("/cdn-cgi/challenge-platform"))),
      new Signal("turnstile", "Turnstile widget", (h, c, u) -> u.stream().anyMatch(x -> x.contains("challenges.cloudflare.com") && x.contains("turnstile"))),
      new Signal("datadome", "x-datadome header", (h, c, u) -> h.containsKey("x-datadome") || h.containsKey("x-dd-b")),
      new Signal("datadome", "datadome cookie", (h, c, u) -> c.contains("datadome")),
      new Signal("datadome", "DataDome host (captcha-delivery.com, datadome.co)", (h, c, u) -> anyHost(u, "captcha-delivery\\.com|datadome\\.co")),
      new Signal("akamai", "_abck / bm_sz cookie", (h, c, u) -> c.stream().anyMatch(x -> List.of("_abck", "bm_sz", "ak_bmsc", "bm_sv").contains(x))),
      new Signal("akamai", "akamai header", (h, c, u) -> h.keySet().stream().anyMatch(k -> k.startsWith("akamai") || k.startsWith("x-akamai"))),
      new Signal("human", "_px cookie", (h, c, u) -> c.stream().anyMatch(x -> x.startsWith("_px"))),
      new Signal("human", "HUMAN host (px-cdn.net, px-cloud.net)", (h, c, u) -> anyHost(u, "px-cdn\\.net|px-cloud\\.net|perimeterx\\.net|pxchk\\.net")),
      new Signal("kasada", "x-kpsdk header", (h, c, u) -> h.keySet().stream().anyMatch(k -> k.startsWith("x-kpsdk"))),
      new Signal("kasada", "ips.js / kpsdk request", (h, c, u) -> u.stream().anyMatch(x -> x.contains("/ips.js") || x.contains("kpsdk"))),
      new Signal("imperva", "x-iinfo header", (h, c, u) -> h.containsKey("x-iinfo")),
      new Signal("imperva", "incap_ses / visid_incap cookie", (h, c, u) -> c.stream().anyMatch(x -> x.matches("^(incap_ses|visid_incap|nlbi_).*"))),
      new Signal("awswaf", "aws-waf-token cookie", (h, c, u) -> c.contains("aws-waf-token")),
      new Signal("awswaf", "AWS WAF host (awswaf.com)", (h, c, u) -> anyHost(u, "awswaf\\.com")),
      new Signal("recaptcha", "reCAPTCHA script", (h, c, u) -> u.stream().anyMatch(x -> x.contains("google.com/recaptcha") || x.contains("recaptcha.net"))),
      new Signal("hcaptcha", "hCaptcha host", (h, c, u) -> anyHost(u, "hcaptcha\\.com")));
  static final Pattern CHALLENGE = Pattern.compile("just a moment|checking your browser|verify you are human|press & hold|press and hold|are you a robot|human touch", Pattern.CASE_INSENSITIVE);
  static final Pattern BLOCKED = Pattern.compile("access denied|access to this page has been denied|attention required|you have been blocked|request blocked|error 1020|pardon our interruption", Pattern.CASE_INSENSITIVE);

  // One load in a fresh context: DOM ready + 6 s for the protection scripts to run, then the main
  // response's headers, the cookies and every requested URL.
  static Object[] survey(Browser browser, String url) {
    BrowserContext context = browser.newContext();
    Page tab = context.newPage();
    List<String> urls = Collections.synchronizedList(new ArrayList<>());
    tab.onRequest(r -> urls.add(r.url()));
    Map<String, String> h = new HashMap<>();
    String outcome;
    try {
      Response r = tab.navigate(url, new Page.NavigateOptions().setWaitUntil(WaitUntilState.DOMCONTENTLOADED).setTimeout(45000));
      int status = r != null ? r.status() : 0;
      if (r != null) h = r.allHeaders();
      tab.waitForTimeout(6000);
      String title = tab.title();
      String text;
      try { text = tab.locator("body").innerText().replaceAll("\\s+", " "); } catch (PlaywrightException e) { text = ""; }
      StringBuilder frames = new StringBuilder();
      for (Frame f : tab.frames()) frames.append(f.url()).append(' ');
      if (CHALLENGE.matcher(title + " " + text.substring(0, Math.min(800, text.length()))).find() || frames.toString().contains("captcha-delivery.com")) outcome = "challenge";
      else if (BLOCKED.matcher(title + " " + text.substring(0, Math.min(400, text.length()))).find() || (List.of(403, 429, 444).contains(status) && text.length() < 1500)) outcome = "blocked";
      else outcome = "content";
    } catch (PlaywrightException e) {
      outcome = "error";
    }
    List<String> cookies = new ArrayList<>();
    for (Cookie c : context.cookies()) cookies.add(c.name);
    context.close();
    List<String> snapshot;
    synchronized (urls) { snapshot = new ArrayList<>(urls); }
    List<Signal> hits = new ArrayList<>();
    for (Signal s : SIGNALS) if (s.test().check(h, cookies, snapshot)) hits.add(s);
    return new Object[] {outcome, hits};
  }

  public static void main(String[] args) throws Exception {
    String body = "{\"proxy\": " + new Gson().toJson(System.getenv("PROXY_URL")) + ", \"headless\": true}";
    HttpResponse<String> res = HttpClient.newHttpClient().send(HttpRequest.newBuilder(URI.create("https://starter.cdpfleet.com/chromium/session"))
        .header("x-api-key", KEY).header("content-type", "application/json")
        .POST(HttpRequest.BodyPublishers.ofString(body)).build(), HttpResponse.BodyHandlers.ofString());
    if (res.statusCode() != 200) throw new RuntimeException("launch: " + res.statusCode() + " " + res.body());
    String wsUrl = JsonParser.parseString(res.body()).getAsJsonObject().get("wsUrl").getAsString();

    try (Playwright playwright = Playwright.create()) {
      Browser browser = playwright.chromium().connect(wsUrl, new BrowserType.ConnectOptions().setHeaders(Map.of("x-api-key", KEY)));
      try {
        Map<String, JsonObject> vendors = new LinkedHashMap<>();
        for (String v : VENDORS) {
          JsonObject o = new JsonObject();
          o.addProperty("vendor", v);
          o.addProperty("pages", 0);
          o.add("signals", new JsonObject());
          JsonObject oc = new JsonObject();
          for (String k : List.of("content", "challenge", "blocked", "error")) oc.addProperty(k, 0);
          o.add("outcomes", oc);
          vendors.put(v, o);
        }
        JsonArray demoPages = new JsonArray();
        int none = 0, expectedMatched = 0;
        for (String[] page : PAGES) {
          Object[] r = survey(browser, page[0]);
          String outcome = (String) r[0];
          @SuppressWarnings("unchecked") List<Signal> hits = (List<Signal>) r[1];
          LinkedHashSet<String> found = new LinkedHashSet<>();
          for (Signal s : hits) found.add(s.vendor());
          if (found.isEmpty()) none++;
          if (found.contains(page[1])) expectedMatched++;
          for (String v : found) {
            JsonObject o = vendors.get(v);
            o.addProperty("pages", o.get("pages").getAsInt() + 1);
            JsonObject oc = o.getAsJsonObject("outcomes");
            oc.addProperty(outcome, oc.get(outcome).getAsInt() + 1);
          }
          for (Signal s : hits) {
            JsonObject sig = vendors.get(s.vendor()).getAsJsonObject("signals");
            sig.addProperty(s.label(), (sig.has(s.label()) ? sig.get(s.label()).getAsInt() : 0) + 1);
          }
          if (page[2] != null) {
            JsonObject d = new JsonObject();
            d.addProperty("page", page[2]);
            JsonArray vs = new JsonArray();
            for (String v : found) vs.add(v);
            d.add("vendors", vs);
            d.addProperty("outcome", outcome);
            demoPages.add(d);
          }
        }
        JsonObject out = new JsonObject();
        out.addProperty("measured_at", LocalDate.now(ZoneOffset.UTC).toString());
        out.addProperty("pages", PAGES.length);
        out.addProperty("no_vendor_found", none);
        out.addProperty("expected_vendor_found", expectedMatched);
        JsonArray vs = new JsonArray();
        for (JsonObject o : vendors.values()) if (o.get("pages").getAsInt() > 0) vs.add(o);
        out.add("vendors", vs);
        out.add("demo_pages", demoPages);
        System.out.println(new GsonBuilder().setPrettyPrinting().disableHtmlEscaping().create().toJson(out));
      } finally {
        browser.close();
      }
    }
  }
}
