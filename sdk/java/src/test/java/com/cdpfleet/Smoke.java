// SDK smoke test on production: launch → connect → page → live link → keep_alive reconnect → close.
// env: CDPFLEET_API_KEY (a dedicated plan for keep_alive), PROXY_URL. Prints one JSON line.
// Run: mvn -q test-compile exec:java
package com.cdpfleet;

import com.google.gson.JsonElement;
import com.google.gson.JsonObject;
import com.microsoft.playwright.Browser;
import com.microsoft.playwright.BrowserType;
import com.microsoft.playwright.Page;
import com.microsoft.playwright.Playwright;

import java.util.HashMap;
import java.util.Map;

public class Smoke {
  static Map<String, Object> options(Object... kv) {
    Map<String, Object> m = new HashMap<>();
    if (System.getenv("PROXY_URL") != null) m.put("proxy", System.getenv("PROXY_URL"));
    for (int i = 0; i < kv.length; i += 2) m.put((String) kv[i], kv[i + 1]);
    return m;
  }

  public static void main(String[] args) {
    try (Playwright pw = Playwright.create()) {
      BrowserType chromium = pw.chromium();
      Cdpfleet fleet = new Cdpfleet();
      JsonObject out = new JsonObject();

      Session s1 = fleet.launch("chromium", options("headless", true, "live_view", true));
      Browser b1 = s1.connect(chromium);
      Page p1 = b1.newPage();
      p1.navigate("https://httpbin.org/html");
      out.addProperty("title", p1.locator("h1").textContent());
      out.addProperty("live_protocol", s1.liveLink("view").get("protocol").getAsString());
      s1.close();

      Session s2 = fleet.launch("chromium", options("headless", true, "keep_alive", true, "inactivity_timeout", "2m"));
      Browser b2 = s2.connect(chromium);
      Page p2 = b2.newContext().newPage();
      p2.navigate("https://httpbin.org/html");
      b2.close();
      b2 = s2.connect(chromium);
      out.addProperty("keep_alive_page_kept", b2.contexts().stream()
          .flatMap((c) -> c.pages().stream()).anyMatch((p) -> p.url().contains("httpbin")));
      s2.close();
      out.addProperty("ended", !fleet.threads().toString().contains(s2.id()));
      JsonElement used = fleet.usage().get("used_thread_seconds");
      out.addProperty("usage_ok", used != null && used.isJsonPrimitive() && used.getAsJsonPrimitive().isNumber());
      System.out.println(out);
    }
  }
}
