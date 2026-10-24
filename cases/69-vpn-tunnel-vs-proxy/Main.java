// Maven: com.microsoft.playwright:playwright:1.60.0, com.google.code.gson:gson:2.11.0
// Run with PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1.
// env: CDPFLEET_API_KEY (sticky US exits from the cdpfleet residential proxy, Tier 1; one load per bot-protection page)
import com.google.gson.*;
import com.microsoft.playwright.*;
import com.microsoft.playwright.options.WaitUntilState;
import java.net.URI;
import java.net.http.*;
import java.security.SecureRandom;
import java.time.Instant;
import java.util.*;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");
  static final Gson GSON = new GsonBuilder().setPrettyPrinting().disableHtmlEscaping().serializeNulls().create();
  static final HttpClient HTTP = HttpClient.newHttpClient();
  static final String RUN = String.format("%06x", new SecureRandom().nextInt(1 << 24));

  record Setup(String id, String name, String engine, String type, boolean headless) {}
  record Network(String label, boolean tunnel) {}

  // One sticky exit per setup, used with and without the tunnel: the only difference between the two rows is vpn_tunnel.
  static String exitFor(String id) { return "cdpfleet-resi-country-us-session-t" + id + RUN + "-lifetime-30"; }
  static final List<Setup> SETUPS = List.of(
      new Setup("c", "Google Chrome, headful", "chrome", "chromium", false),
      new Setup("f", "Camoufox, defaults", "camoufox", "firefox", true));
  static final List<Network> NETWORKS = List.of(new Network("residential proxy", false), new Network("residential proxy + vpn_tunnel", true));

  // The WebRTC leak test from case 54: ICE candidates against a public STUN server. A "srflx" candidate is the public IP
  // the STUN server saw. The same expression runs in every language.
  static final String GATHER = """
      async () => {
        if (typeof RTCPeerConnection === 'undefined') return { api: false, candidates: [] };
        const pc = new RTCPeerConnection({ iceServers: [{ urls: 'stun:stun.l.google.com:19302' }] });
        const candidates = [];
        pc.onicecandidate = (e) => { if (e.candidate && e.candidate.candidate) candidates.push(e.candidate.candidate); };
        pc.createDataChannel('probe');
        await pc.setLocalDescription(await pc.createOffer());
        await new Promise((done) => {
          const timer = setTimeout(done, 8000);
          pc.onicegatheringstatechange = () => { if (pc.iceGatheringState === 'complete') { clearTimeout(timer); done(); } };
        });
        pc.close();
        return { api: true, candidates };
      }""";
  // The protocol of the page's own request: h2 over TCP, or h3 over QUIC (UDP).
  static final String PROTOCOL = "performance.getEntriesByType('navigation')[0]?.nextHopProtocol || null";
  // Cloudflare's interstitial or block page, or the real page.
  static final String CF_STATE = """
      (() => {
        const t = (document.title + ' ' + (document.body ? document.body.innerText : '').slice(0, 400)).replace(/\\s+/g, ' ');
        return /sorry, you have been blocked|attention required|error 1020|access denied/i.test(t) ? 'blocked'
          : /just a moment|performing security verification|verify you are human|checking your browser/i.test(t) ? 'challenge' : 'page';
      })()""";
  // DataDome's iframe from captcha-delivery.com: a device check, a CAPTCHA, or a block (t=bv) — as in case 65.
  static final String DD_STATE = """
      (() => {
        const f = [...document.querySelectorAll('iframe')].map((i) => i.src).find((s) => /captcha-delivery\\.com/.test(s)) || '';
        const text = (document.body ? document.body.innerText : '').trim();
        return /[?&]t=bv(&|$)/.test(f) ? 'blocked' : /\\/captcha\\//.test(f) ? 'challenge' : f ? 'checking' : text.length > 200 ? 'page' : 'loading';
      })()""";
  static final Page.NavigateOptions NAV = new Page.NavigateOptions().setWaitUntil(WaitUntilState.DOMCONTENTLOADED).setTimeout(45000);

  static Playwright playwright;

  // 503 means the fleet is momentarily busy: wait Retry-After and try again.
  static Browser launch(Setup s, Network n) throws Exception {
    JsonObject body = new JsonObject();
    body.addProperty("proxy", exitFor(s.id()));
    body.addProperty("headless", s.headless());
    body.addProperty("record", false);
    if (n.tunnel()) body.addProperty("vpn_tunnel", true);
    for (int attempt = 1; ; attempt++) {
      HttpResponse<String> res = HTTP.send(HttpRequest.newBuilder(URI.create("https://starter.cdpfleet.com/" + s.engine() + "/session"))
          .header("x-api-key", KEY).header("content-type", "application/json")
          .POST(HttpRequest.BodyPublishers.ofString(body.toString())).build(), HttpResponse.BodyHandlers.ofString());
      if (res.statusCode() == 200) {
        String wsUrl = JsonParser.parseString(res.body()).getAsJsonObject().get("wsUrl").getAsString();
        BrowserType type = s.type().equals("firefox") ? playwright.firefox() : playwright.chromium();
        return type.connect(wsUrl, new BrowserType.ConnectOptions().setHeaders(Map.of("x-api-key", KEY)));
      }
      if ((res.statusCode() != 429 && res.statusCode() != 503) || attempt == 6) throw new RuntimeException("launch " + res.statusCode() + " " + res.body());
      Thread.sleep(Long.parseLong(res.headers().firstValue("retry-after").orElse("5")) * 1000);
    }
  }

  // Residential peers drop a connection now and then: one retry.
  static void go(Page tab, String url) {
    try { tab.navigate(url, NAV); } catch (PlaywrightException e) { tab.navigate(url, NAV); }
  }

  static String str(Page tab, String expr) {
    try { Object v = tab.evaluate(expr); return v instanceof String s ? s : null; } catch (PlaywrightException e) { return null; }
  }

  static JsonObject cell(String outcome, Integer seconds) {
    JsonObject c = new JsonObject();
    c.addProperty("outcome", outcome);
    c.addProperty("seconds", seconds);
    return c;
  }

  // One load, then watch the page once a second for 20 s (Cloudflare) or 12 s (DataDome) without touching it.
  static JsonObject watch(BrowserContext context, String url, String kind) throws Exception {
    Page tab = context.newPage();
    long t0 = System.currentTimeMillis();
    try {
      try { go(tab, url); } catch (PlaywrightException e) { return cell("error", null); }
      boolean seen = false;
      long until = System.currentTimeMillis() + (kind.equals("cf") ? 20000 : 12000);
      while (System.currentTimeMillis() < until) {
        String s = str(tab, kind.equals("cf") ? CF_STATE : DD_STATE);
        if ("blocked".equals(s)) return cell("blocked", null);
        if ("challenge".equals(s) && kind.equals("dd")) return cell("challenge", null);
        if ("challenge".equals(s) || "checking".equals(s)) seen = true;
        if ("page".equals(s)) return seen ? cell("cleared", (int) Math.round((System.currentTimeMillis() - t0) / 1000.0)) : cell("content", null);
        Thread.sleep(1000);
      }
      return cell(seen ? "challenge" : "error", null);
    } finally {
      tab.close();
    }
  }

  static JsonObject row(Setup s, Network n) throws Exception {
    Browser browser = launch(s, n);
    JsonObject out = new JsonObject();
    out.addProperty("setup", s.name());
    out.addProperty("network", n.label());
    try {
      BrowserContext context = browser.newContext();
      Page tab = context.newPage();
      go(tab, "https://api.ipify.org/?format=json");
      out.addProperty("exit_ip", JsonParser.parseString(tab.textContent("body")).getAsJsonObject().get("ip").getAsString());
      Map<?, ?> g = (Map<?, ?>) tab.evaluate("(" + GATHER + ")()");
      List<String> ips = new ArrayList<>();
      boolean sawMdns = false;
      for (Object c : (List<?>) g.get("candidates")) {
        // candidate:<foundation> <component> <transport> <priority> <address> <port> typ <type> …
        String[] f = ((String) c).split(" ");
        if (f.length < 8) continue;
        if (f[4].endsWith(".local")) sawMdns = true;
        if (f[4].matches("\\d+\\.\\d+\\.\\d+\\.\\d+") && !f[4].equals("0.0.0.0") && !ips.contains(f[4])) ips.add(f[4]);
      }
      out.addProperty("webrtc_api", (Boolean) g.get("api"));
      JsonArray ipArr = new JsonArray();
      ips.forEach(ipArr::add);
      out.add("webrtc_ips", ipArr);
      out.addProperty("mdns_only", ips.isEmpty() && sawMdns);
      tab.close();
      // HTTP/3: a page announces it over HTTP/2 (Alt-Svc); the browser can switch to QUIC on a later request. Four loads
      // in fresh tabs, a pause between them.
      JsonArray protocols = new JsonArray();
      for (int i = 0; i < 4; i++) {
        Page t = context.newPage();
        try { go(t, "https://cloudflare-quic.com/"); } catch (PlaywrightException e) { /* recorded as null */ }
        String p = str(t, PROTOCOL);
        if (p == null) protocols.add(JsonNull.INSTANCE); else protocols.add(p);
        t.close();
        Thread.sleep(2500);
      }
      out.add("protocols", protocols);
      context.close();
      // The bot-protection pages, each in a fresh context: Cloudflare's challenge test page (case 64) and DataDome's
      // own site (case 65).
      String[][] pages = { { "scrapingcourse", "https://www.scrapingcourse.com/cloudflare-challenge", "cf" }, { "datadome", "https://datadome.co/", "dd" } };
      for (String[] pg : pages) {
        BrowserContext c = browser.newContext();
        out.add(pg[0], watch(c, pg[1], pg[2]));
        c.close();
      }
    } finally {
      browser.close();
    }
    return out;
  }

  public static void main(String[] args) throws Exception {
    JsonObject result = new JsonObject();
    try (Playwright pw = Playwright.create()) {
      playwright = pw;
      JsonArray rows = new JsonArray();
      for (Setup s : SETUPS)
        for (Network n : NETWORKS) rows.add(row(s, n));
      result.addProperty("measured_at", Instant.now().toString().substring(0, 10));
      result.add("rows", rows);
    }
    System.out.println(GSON.toJson(result));
  }
}
