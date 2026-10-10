# cdpfleet for Java

Launch a remote browser on [cdpfleet](https://cdpfleet.com) and connect your own Playwright to it.
Full docs: https://cdpfleet.com/docs/sdk

```xml
<dependency>
  <groupId>com.cdpfleet</groupId>
  <artifactId>cdpfleet</artifactId>
  <version>0.1.0</version>
</dependency>
<!-- bring your own Playwright -->
<dependency>
  <groupId>com.microsoft.playwright</groupId>
  <artifactId>playwright</artifactId>
  <version>1.60.0</version>
</dependency>
```

```java
import com.cdpfleet.Cdpfleet;
import com.cdpfleet.Session;
import com.microsoft.playwright.*;
import java.util.Map;

Cdpfleet fleet = new Cdpfleet(); // reads CDPFLEET_API_KEY
try (Playwright pw = Playwright.create();
     Session session = fleet.launch("chrome", Map.of("proxy", System.getenv("PROXY_URL"), "headless", true))) {
  Browser browser = session.connect(pw.chromium());
  Page page = browser.newPage();
  page.navigate("https://example.com");
  System.out.println(page.title());
}
```

`launch` retries fleet backpressure (503) and rate limits (429) for up to `maxWait` (120 s);
other errors throw `CdpfleetException` with `status()`, `code()` and `body()`.
Options: `new Cdpfleet(new Cdpfleet.Options().setApiKey(...).setMaxWait(60))`.

## keep_alive

A session launched with `keep_alive: true` survives a disconnect: call `session.connect(...)`
again to re-attach to the same browser, pages and all. `session.close()` then ends it for good
(for other sessions closing the browser is enough). Account API: `fleet.me()`, `usage()`,
`threads()`, `sessions(days, limit)`, `recordings()`, `proxyBalance()`, `endSession(id)`.
