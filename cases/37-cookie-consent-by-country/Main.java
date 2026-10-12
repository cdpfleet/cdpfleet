// Maven: com.microsoft.playwright:playwright:1.60.0, com.google.code.gson:gson:2.11.0
// Run with PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1. env: CDPFLEET_API_KEY, PROXY_URL_DE (a German proxy), PROXY_URL_US (a US proxy)
import com.google.gson.*;
import com.microsoft.playwright.*;
import com.microsoft.playwright.options.WaitForSelectorState;
import com.microsoft.playwright.options.WaitUntilState;
import java.net.URI;
import java.net.http.*;
import java.util.*;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");
  static final Gson GSON = new GsonBuilder().serializeNulls().create();
  static final List<String> SITES = List.of("https://www.theguardian.com/international", "https://www.bbc.com/", "https://apnews.com/", "https://www.theverge.com/", "https://www.huffpost.com/");
  // First-layer buttons of the two consent platforms these sites use. OneTrust renders in the
  // page; Sourcepoint in an iframe. Only a free "reject" counts: "Reject all and subscribe" doesn't.
  static final Map<String, String> REJECT = Map.of("OneTrust", "#onetrust-reject-all-handler", "Sourcepoint", "button[title=\"Reject all\"], button[title=\"I do not agree\"]");
  static final Map<String, String> ACCEPT = Map.of("OneTrust", "#onetrust-accept-btn-handler", "Sourcepoint", "button[title=\"Accept all\"], button[title=\"I agree\"]");
  static final String SP_FRAME = "iframe[id^=\"sp_message_iframe\"]";
  static final String BANNER = "#onetrust-banner-sdk, iframe[id^=\"sp_message_iframe\"]";

  // Which consent platform the page runs, whether its banner is showing, and which consent
  // APIs it exposes (__tcfapi: IAB Europe's TCF; __gpp: IAB Tech Lab's GPP, used for US state
  // privacy laws).
  static final String PROBE = """
      () => {
        const shown = (s) => [...document.querySelectorAll(s)].some((e) => e.getClientRects().length && getComputedStyle(e).visibility !== 'hidden');
        const cmp = window.OneTrust || document.querySelector('#onetrust-consent-sdk') ? 'OneTrust'
          : window._sp_ || document.querySelector('iframe[id^="sp_message_iframe"]') ? 'Sourcepoint'
          : window.Didomi ? 'Didomi' : window.Cookiebot ? 'Cookiebot' : window.UC_UI ? 'Usercentrics' : null;
        return { cmp, banner: shown('#onetrust-banner-sdk, iframe[id^="sp_message_iframe"]'),
          tcf: typeof window.__tcfapi === 'function', gpp: typeof window.__gpp === 'function' };
      }""";

  static Browser launch(Playwright playwright, String proxy) throws Exception {
    String body = "{\"proxy\": " + GSON.toJson(proxy) + ", \"headless\": false}";
    HttpResponse<String> res = HttpClient.newHttpClient().send(HttpRequest.newBuilder(URI.create("https://starter.cdpfleet.com/chromium/session"))
        .header("x-api-key", KEY).header("content-type", "application/json")
        .POST(HttpRequest.BodyPublishers.ofString(body)).build(), HttpResponse.BodyHandlers.ofString());
    if (res.statusCode() / 100 != 2) throw new RuntimeException("launch: " + res.statusCode() + " " + res.body());
    String wsUrl = JsonParser.parseString(res.body()).getAsJsonObject().get("wsUrl").getAsString();
    return playwright.chromium().connect(wsUrl, new BrowserType.ConnectOptions().setHeaders(Map.of("x-api-key", KEY)));
  }

  // Request URLs aren't always valid java.net.URI input (unescaped {} or "), so cut the host out.
  static String hostOf(String url) {
    return url.replaceFirst("^[a-zA-Z][a-zA-Z0-9+.-]*://", "").split("[/?#:]", 2)[0];
  }

  static String domainOf(String url) {
    String[] parts = hostOf(url).split("\\.");
    return parts[parts.length - 2] + "." + parts[parts.length - 1];
  }

  static boolean onDomain(String host, String domain) {
    return host.equals(domain) || host.endsWith("." + domain);
  }

  // Clicks a consent button; true if the click went through (one retry: a consent iframe that
  // re-renders can swallow a click).
  static boolean press(Locator button) {
    for (int attempt = 1; attempt <= 2; attempt++) {
      try {
        button.click(new Locator.ClickOptions().setTimeout(10000));
        return true;
      } catch (PlaywrightException e) {
        // retry once
      }
    }
    return false;
  }

  static JsonObject measure(BrowserContext ctx, Set<String> hosts, String domain) {
    List<com.microsoft.playwright.options.Cookie> cookies = ctx.cookies();
    JsonObject m = new JsonObject();
    m.addProperty("cookies", cookies.size());
    m.addProperty("third_party_cookies", cookies.stream().filter(c -> !onDomain(c.domain.replaceFirst("^\\.", ""), domain)).count());
    m.addProperty("other_hosts", hosts.size());
    return m;
  }

  // Load a site in a fresh context, then optionally click a consent button. Counts cookies and
  // the hosts on other domains the page contacted (ads, analytics, CDNs) up to each moment.
  // Returns [before, after, missing]; after is null (and missing says why) when there was
  // nothing to click.
  static JsonElement[] visit(Browser browser, String url, String choice) {
    String domain = domainOf(url);
    BrowserContext ctx = browser.newContext(new Browser.NewContextOptions().setViewportSize(1280, 900));
    Page page = ctx.newPage();
    Set<String> hosts = new HashSet<>();
    page.onRequest(r -> {
      String h = r.url().startsWith("http") ? hostOf(r.url()) : "";
      if (!h.isEmpty() && !onDomain(h, domain)) hosts.add(h);
    });
    try {
      Page.NavigateOptions load = new Page.NavigateOptions().setWaitUntil(WaitUntilState.DOMCONTENTLOADED).setTimeout(60000);
      Response res;
      try {
        res = page.navigate(url, load);
      } catch (PlaywrightException e) { // proxies drop a connection now and then: one retry
        res = page.navigate(url, load);
      }
      // Consent scripts load late on busy pages: wait up to 20 s for the banner, and give
      // every page at least 6 s before measuring.
      long t0 = System.nanoTime();
      Long bannerAfterMs = null;
      try {
        page.locator(BANNER).first().waitFor(new Locator.WaitForOptions().setState(WaitForSelectorState.VISIBLE).setTimeout(20000));
        bannerAfterMs = (System.nanoTime() - t0) / 1_000_000;
      } catch (PlaywrightException e) {
        // no banner
      }
      long elapsed = (System.nanoTime() - t0) / 1_000_000;
      if (elapsed < 6000) page.waitForTimeout(6000 - elapsed);
      JsonObject probe = GSON.toJsonTree(page.evaluate("(" + PROBE + ")()")).getAsJsonObject();
      JsonObject before = new JsonObject();
      if (res == null) before.add("status", JsonNull.INSTANCE); else before.addProperty("status", res.status());
      for (Map.Entry<String, JsonElement> e : probe.entrySet()) before.add(e.getKey(), e.getValue());
      before.addProperty("banner_after_ms", bannerAfterMs);
      for (Map.Entry<String, JsonElement> e : measure(ctx, hosts, domain).entrySet()) before.add(e.getKey(), e.getValue());
      if (choice == null) return new JsonElement[] { before, JsonNull.INSTANCE, JsonNull.INSTANCE };
      JsonPrimitive missing = new JsonPrimitive(probe.get("banner").getAsBoolean() ? "no " + choice + " button on the first layer" : "no banner");
      String cmp = probe.get("cmp").isJsonNull() ? null : probe.get("cmp").getAsString();
      String selector = cmp == null ? null : (choice.equals("reject") ? REJECT : ACCEPT).get(cmp);
      if (selector == null) return new JsonElement[] { before, JsonNull.INSTANCE, missing };
      Locator button = "Sourcepoint".equals(cmp) ? page.frameLocator(SP_FRAME).locator(selector).first() : page.locator(selector).first();
      try {
        button.waitFor(new Locator.WaitForOptions().setState(WaitForSelectorState.VISIBLE).setTimeout(10000));
      } catch (PlaywrightException e) {
        return new JsonElement[] { before, JsonNull.INSTANCE, missing };
      }
      String title = button.getAttribute("title");
      String label = title != null ? title : button.innerText().trim();
      page.waitForTimeout(2000); // let the banner finish animating in, or the click can be lost
      if (!press(button)) return new JsonElement[] { before, JsonNull.INSTANCE, new JsonPrimitive("click failed") };
      int clicks = 1;
      try { // if the banner is still up, the click was lost: click once more
        page.locator(BANNER).first().waitFor(new Locator.WaitForOptions().setState(WaitForSelectorState.HIDDEN).setTimeout(5000));
      } catch (PlaywrightException e) {
        clicks = 2;
        press(button);
      }
      page.waitForTimeout(6000);
      JsonObject after = new JsonObject();
      after.addProperty("button", label);
      after.addProperty("clicks", clicks);
      for (Map.Entry<String, JsonElement> e : measure(ctx, hosts, domain).entrySet()) after.add(e.getKey(), e.getValue());
      return new JsonElement[] { before, after, JsonNull.INSTANCE };
    } finally {
      ctx.close();
    }
  }

  // A site may refuse one exit IP (an error status): visit again in a fresh context, whose new
  // proxy connection gets another IP from the rotating proxy.
  static JsonElement[] visitOnce(Browser browser, String url, String choice) {
    JsonElement[] r = visit(browser, url, choice);
    JsonElement status = r[0].getAsJsonObject().get("status");
    return !status.isJsonNull() && status.getAsInt() / 100 == 2 ? r : visit(browser, url, choice);
  }

  public static void main(String[] args) throws Exception {
    try (Playwright playwright = Playwright.create()) {
      // Sessions on shared threads end after 60 s without traffic, so the countries run one after the other.
      List<JsonElement[]> fromUs = new ArrayList<>();
      Browser us = launch(playwright, System.getenv("PROXY_URL_US"));
      try {
        for (String url : SITES) fromUs.add(visitOnce(us, url, null));
      } finally {
        us.close();
      }
      Browser de = launch(playwright, System.getenv("PROXY_URL_DE"));
      try {
        JsonArray sites = new JsonArray();
        for (int i = 0; i < SITES.size(); i++) {
          String url = SITES.get(i);
          JsonElement[] rejected = visitOnce(de, url, "reject");
          JsonElement[] accepted = visitOnce(de, url, "accept");
          JsonObject site = new JsonObject();
          site.addProperty("site", hostOf(url).replaceFirst("^www\\.", ""));
          site.add("us", fromUs.get(i)[0]);
          site.add("de", rejected[0]);
          site.add("de_reject", rejected[1]);
          site.add("de_reject_missing", rejected[2]);
          site.add("de_accept", accepted[1]);
          site.add("de_accept_missing", accepted[2]);
          sites.add(site);
        }
        JsonObject out = new JsonObject();
        out.add("sites", sites);
        System.out.println(new GsonBuilder().serializeNulls().setPrettyPrinting().disableHtmlEscaping().create().toJson(out));
      } finally {
        de.close();
      }
    }
  }
}
