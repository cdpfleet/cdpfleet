// Maven: com.microsoft.playwright:playwright:1.60.0, com.google.code.gson:gson:2.11.0
// Run with PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1.
// env: CDPFLEET_API_KEY (each setup gets its own sticky US exit from the cdpfleet residential proxy; a few KB of traffic)
import com.google.gson.*;
import com.microsoft.playwright.*;
import java.net.URI;
import java.net.http.*;
import java.util.*;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");
  static final Gson GSON = new GsonBuilder().setPrettyPrinting().disableHtmlEscaping().serializeNulls().create();
  static final HttpClient HTTP = HttpClient.newHttpClient();

  // What a WebRTC leak test runs: gather ICE candidates against a public STUN server. A "srflx"
  // candidate carries the public IP the STUN server saw; UDP doesn't go through an HTTP/SOCKS proxy —
  // on cdpfleet, proxied sessions can't send UDP at all, so only an anonymous mDNS host candidate is left.
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

  record Setup(String name, String engine, String body) {}

  static final List<Setup> SETUPS = List.of(
      new Setup("Chromium", "chromium", "{\"headless\":\"new\"}"),
      new Setup("Google Chrome", "chrome", "{\"headless\":\"new\"}"),
      new Setup("Firefox", "firefox", "{\"headless\":true}"),
      new Setup("Camoufox (defaults: geoip on)", "camoufox", "{\"headless\":true}"),
      new Setup("Camoufox, block_webrtc: true", "camoufox", "{\"headless\":true,\"block_webrtc\":true}"));

  // A sticky residential exit per setup: one IP for the whole session, to compare with.
  static final String RUN = Long.toString(new Random().nextLong() & Long.MAX_VALUE, 36).substring(0, 6);
  static String stickyExit(int i) {
    return "cdpfleet-resi-country-us-session-w54" + RUN + i + "-lifetime-10";
  }

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

  // Residential peers drop a connection now and then: up to two retries.
  static void go(Page page, String url) throws Exception {
    for (int attempt = 1; ; attempt++) {
      try {
        page.navigate(url, new Page.NavigateOptions().setTimeout(60000));
        return;
      } catch (PlaywrightException e) {
        if (attempt == 3) throw e;
        Thread.sleep(2000);
      }
    }
  }

  public static void main(String[] args) throws Exception {
    JsonArray results = new JsonArray();
    try (Playwright pw = Playwright.create()) {
      for (int i = 0; i < SETUPS.size(); i++) {
        Setup s = SETUPS.get(i);
        JsonObject body = JsonParser.parseString(s.body()).getAsJsonObject();
        body.addProperty("proxy", stickyExit(i));
        body.addProperty("record", false);
        JsonObject session = launch(s.engine(), body);
        BrowserType type = List.of("firefox", "camoufox").contains(s.engine()) ? pw.firefox() : pw.chromium();
        Browser browser = type.connect(session.get("wsUrl").getAsString(), new BrowserType.ConnectOptions().setHeaders(Map.of("x-api-key", KEY)));
        try {
          BrowserContext context = browser.newContext();
          Page page = context.newPage();
          go(page, "https://api.ipify.org/?format=json");
          String exitIp = JsonParser.parseString(page.textContent("body")).getAsJsonObject().get("ip").getAsString();
          JsonObject gathered = GSON.toJsonTree(page.evaluate(GATHER)).getAsJsonObject();
          JsonArray candidates = gathered.getAsJsonArray("candidates");
          Set<String> publicIps = new LinkedHashSet<>();
          int mdns = 0;
          for (JsonElement c : candidates) {
            // candidate:<foundation> <component> <transport> <priority> <address> <port> typ <type> …
            String[] f = c.getAsString().split(" ");
            if (f[7].equals("host") && f[4].endsWith(".local")) mdns++;
            if (f[4].matches("\\d+\\.\\d+\\.\\d+\\.\\d+") && !f[4].equals("0.0.0.0")) publicIps.add(f[4]);
          }
          JsonObject r = new JsonObject();
          r.addProperty("setup", s.name());
          r.addProperty("exit_ip", exitIp);
          r.addProperty("webrtc_api", gathered.get("api").getAsBoolean());
          r.addProperty("candidates", candidates.size());
          r.addProperty("mdns_host_candidates", mdns);
          JsonArray ips = new JsonArray();
          publicIps.forEach(ips::add);
          r.add("public_ips", ips);
          r.addProperty("shows_exit_ip", publicIps.contains(exitIp));
          r.addProperty("leaks_other_ip", publicIps.stream().anyMatch((ip) -> !ip.equals(exitIp)));
          results.add(r);
        } finally {
          browser.close();
        }
      }
    }
    System.out.println(GSON.toJson(results));
  }
}
