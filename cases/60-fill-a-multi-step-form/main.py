# pip install playwright==1.60.0 requests
# env: CDPFLEET_API_KEY, PROXY_URL_US
import json
import os
import re
import time

import requests
from playwright.sync_api import sync_playwright

KEY = os.environ["CDPFLEET_API_KEY"]
SITE = "https://www.saucedemo.com"  # a public demo shop for testing practice
PASSWORD = "secret_sauce"  # printed on the shop's login page
# The shop's demo users: a normal one, one whose pages are slow, and one with broken inputs.
USERS = ["standard_user", "performance_glitch_user", "problem_user"]
BUYER = {"firstName": "Ada", "lastName": "Lovelace", "postalCode": "10115"}
ITEMS = ["sauce-labs-backpack", "sauce-labs-bike-light"]


def t(sel):
    return f'[data-test="{sel}"]'


def money(s):
    return float(re.sub(r"[^0-9.]", "", s))


res = requests.post("https://starter.cdpfleet.com/chromium/session", headers={"x-api-key": KEY},
                    json={"proxy": os.environ["PROXY_URL_US"], "headless": True}, timeout=60)
res.raise_for_status()


def checkout(browser, user):
    """The checkout as one user, in a fresh context: every step waits for the page it leads to
    (wait_for_url), never for a fixed time."""
    context = browser.new_context()
    page = context.new_page()
    steps = {}
    mark = [time.time()]

    def step(name):
        steps[name] = round((time.time() - mark[0]) * 1000)
        mark[0] = time.time()

    out = {"user": user, "empty_form_error": None, "typed_back": None, "step_one_error": None, "item_sum": None,
           "subtotal": None, "tax": None, "total": None, "totals_add_up": None, "completed": None}
    try:
        page.goto(SITE)
        page.fill(t("username"), user)
        page.fill(t("password"), PASSWORD)
        page.click(t("login-button"))
        page.wait_for_url("**/inventory.html")
        step("login")

        for item in ITEMS:
            page.click(t(f"add-to-cart-{item}"))
        page.locator(t("shopping-cart-badge")).filter(has_text=str(len(ITEMS))).wait_for()
        page.click(t("shopping-cart-link"))
        page.wait_for_url("**/cart.html")
        page.click(t("checkout"))
        page.wait_for_url("**/checkout-step-one.html")
        step("cart")

        # Step one: an empty form is refused; then type the buyer and read the fields back.
        page.click(t("continue"))
        out["empty_form_error"] = page.locator(t("error")).inner_text()
        page.fill(t("firstName"), BUYER["firstName"])
        page.fill(t("lastName"), BUYER["lastName"])
        page.fill(t("postalCode"), BUYER["postalCode"])
        out["typed_back"] = {k: page.input_value(t(k)) for k in ("firstName", "lastName", "postalCode")}
        page.click(t("continue"))
        # Either step two loads or the form shows an error: wait for whichever comes first.
        page.locator(f'{t("subtotal-label")}, {t("error")}').first.wait_for()
        if "checkout-step-two" not in page.url:
            out["step_one_error"] = page.locator(t("error")).inner_text()
            step("step_one")
            return {**out, "steps_ms": steps}
        step("step_one")

        # Step two: the overview's totals against the item prices.
        prices = page.locator(t("inventory-item-price")).all_inner_texts()
        out["item_sum"] = round(sum(money(p) for p in prices), 2)
        out["subtotal"] = money(page.locator(t("subtotal-label")).inner_text())
        out["tax"] = money(page.locator(t("tax-label")).inner_text())
        out["total"] = money(page.locator(t("total-label")).inner_text())
        out["totals_add_up"] = out["subtotal"] == out["item_sum"] and abs(out["subtotal"] + out["tax"] - out["total"]) < 0.005
        page.click(t("finish"))
        page.wait_for_url("**/checkout-complete.html")
        out["completed"] = page.locator(t("complete-header")).inner_text()
        step("step_two_and_finish")
        return {**out, "steps_ms": steps}
    finally:
        context.close()


with sync_playwright() as p:
    browser = p.chromium.connect(res.json()["wsUrl"], headers={"x-api-key": KEY})
    try:
        print(json.dumps([checkout(browser, u) for u in USERS], indent=2, ensure_ascii=False))
    finally:
        browser.close()
