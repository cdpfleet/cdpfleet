// Maven: com.microsoft.playwright:playwright:1.60.0, com.google.code.gson:gson:2.11.0
// Run with PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1. env: CDPFLEET_API_KEY, PROXY_URL_US
import com.google.gson.*;
import com.microsoft.playwright.*;
import java.net.URI;
import java.net.http.*;
import java.util.*;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");
  static final String SITE = "https://www.saucedemo.com"; // a public demo shop for testing practice
  static final String PASSWORD = "secret_sauce"; // printed on the shop's login page
  // The shop's demo users: a normal one, one whose pages are slow, and one with broken inputs.
  static final List<String> USERS = List.of("standard_user", "performance_glitch_user", "problem_user");
  static final String[][] BUYER = {{"firstName", "Ada"}, {"lastName", "Lovelace"}, {"postalCode", "10115"}};
  static final List<String> ITEMS = List.of("sauce-labs-backpack", "sauce-labs-bike-light");

  static String t(String sel) { return "[data-test=\"" + sel + "\"]"; }
  static double money(String s) { return Double.parseDouble(s.replaceAll("[^0-9.]", "")); }

  // The checkout as one user, in a fresh context: every step waits for the page it leads to
  // (waitForURL), never for a fixed time.
  static JsonObject checkout(Browser browser, String user) {
    BrowserContext context = browser.newContext();
    Page page = context.newPage();
    JsonObject steps = new JsonObject();
    long[] mark = {System.currentTimeMillis()};
    java.util.function.Consumer<String> step = name -> { steps.addProperty(name, System.currentTimeMillis() - mark[0]); mark[0] = System.currentTimeMillis(); };
    JsonObject out = new JsonObject();
    out.addProperty("user", user);
    for (String k : List.of("empty_form_error", "typed_back", "step_one_error", "item_sum", "subtotal", "tax", "total", "totals_add_up", "completed")) out.add(k, JsonNull.INSTANCE);
    try {
      page.navigate(SITE);
      page.fill(t("username"), user);
      page.fill(t("password"), PASSWORD);
      page.click(t("login-button"));
      page.waitForURL("**/inventory.html");
      step.accept("login");

      for (String item : ITEMS) page.click(t("add-to-cart-" + item));
      page.locator(t("shopping-cart-badge")).filter(new Locator.FilterOptions().setHasText(String.valueOf(ITEMS.size()))).waitFor();
      page.click(t("shopping-cart-link"));
      page.waitForURL("**/cart.html");
      page.click(t("checkout"));
      page.waitForURL("**/checkout-step-one.html");
      step.accept("cart");

      // Step one: an empty form is refused; then type the buyer and read the fields back.
      page.click(t("continue"));
      out.addProperty("empty_form_error", page.locator(t("error")).innerText());
      for (String[] f : BUYER) page.fill(t(f[0]), f[1]);
      JsonObject typed = new JsonObject();
      for (String[] f : BUYER) typed.addProperty(f[0], page.inputValue(t(f[0])));
      out.add("typed_back", typed);
      page.click(t("continue"));
      // Either step two loads or the form shows an error: wait for whichever comes first.
      page.locator(t("subtotal-label") + ", " + t("error")).first().waitFor();
      if (!page.url().contains("checkout-step-two")) {
        out.addProperty("step_one_error", page.locator(t("error")).innerText());
        step.accept("step_one");
        out.add("steps_ms", steps);
        return out;
      }
      step.accept("step_one");

      // Step two: the overview's totals against the item prices.
      double sum = 0;
      for (String p : page.locator(t("inventory-item-price")).allInnerTexts()) sum += money(p);
      double itemSum = Math.round(sum * 100) / 100.0;
      double subtotal = money(page.locator(t("subtotal-label")).innerText());
      double tax = money(page.locator(t("tax-label")).innerText());
      double total = money(page.locator(t("total-label")).innerText());
      out.addProperty("item_sum", itemSum);
      out.addProperty("subtotal", subtotal);
      out.addProperty("tax", tax);
      out.addProperty("total", total);
      out.addProperty("totals_add_up", subtotal == itemSum && Math.abs(subtotal + tax - total) < 0.005);
      page.click(t("finish"));
      page.waitForURL("**/checkout-complete.html");
      out.addProperty("completed", page.locator(t("complete-header")).innerText());
      step.accept("step_two_and_finish");
      out.add("steps_ms", steps);
      return out;
    } finally {
      context.close();
    }
  }

  public static void main(String[] args) throws Exception {
    String body = "{\"proxy\": " + new Gson().toJson(System.getenv("PROXY_URL_US")) + ", \"headless\": true}";
    HttpResponse<String> res = HttpClient.newHttpClient().send(HttpRequest.newBuilder(URI.create("https://starter.cdpfleet.com/chromium/session"))
        .header("x-api-key", KEY).header("content-type", "application/json")
        .POST(HttpRequest.BodyPublishers.ofString(body)).build(), HttpResponse.BodyHandlers.ofString());
    if (res.statusCode() != 200) throw new RuntimeException("launch: " + res.statusCode() + " " + res.body());
    String wsUrl = JsonParser.parseString(res.body()).getAsJsonObject().get("wsUrl").getAsString();

    try (Playwright playwright = Playwright.create()) {
      Browser browser = playwright.chromium().connect(wsUrl, new BrowserType.ConnectOptions().setHeaders(Map.of("x-api-key", KEY)));
      try {
        JsonArray runs = new JsonArray();
        for (String user : USERS) runs.add(checkout(browser, user));
        System.out.println(new GsonBuilder().setPrettyPrinting().serializeNulls().disableHtmlEscaping().create().toJson(runs));
      } finally {
        browser.close();
      }
    }
  }
}
