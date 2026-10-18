// npm install playwright@1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL_US
import { chromium } from 'playwright';

const KEY = process.env.CDPFLEET_API_KEY;
const SITE = 'https://www.saucedemo.com'; // a public demo shop for testing practice
const PASSWORD = 'secret_sauce'; // printed on the shop's login page
// The shop's demo users: a normal one, one whose pages are slow, and one with broken inputs.
const USERS = ['standard_user', 'performance_glitch_user', 'problem_user'];
const BUYER = { firstName: 'Ada', lastName: 'Lovelace', postalCode: '10115' };
const ITEMS = ['sauce-labs-backpack', 'sauce-labs-bike-light'];

const res = await fetch('https://starter.cdpfleet.com/chromium/session', {
  method: 'POST',
  headers: { 'x-api-key': KEY, 'content-type': 'application/json' },
  body: JSON.stringify({ proxy: process.env.PROXY_URL_US, headless: true }),
});
if (!res.ok) throw new Error(`launch: ${res.status} ${await res.text()}`);
const { wsUrl } = await res.json();
const browser = await chromium.connect(wsUrl, { headers: { 'x-api-key': KEY } });
const t = (sel) => `[data-test="${sel}"]`;
const money = (s) => Number(String(s).replace(/[^0-9.]/g, ''));

// The checkout as one user, in a fresh context: every step waits for the page it leads to
// (waitForURL), never for a fixed time.
async function checkout(user) {
  const context = await browser.newContext();
  const page = await context.newPage();
  const steps = {};
  let mark = Date.now();
  const step = (name) => { steps[name] = Date.now() - mark; mark = Date.now(); };
  const out = { user, empty_form_error: null, typed_back: null, step_one_error: null, item_sum: null, subtotal: null, tax: null, total: null, totals_add_up: null, completed: null };
  try {
    await page.goto(SITE);
    await page.fill(t('username'), user);
    await page.fill(t('password'), PASSWORD);
    await page.click(t('login-button'));
    await page.waitForURL('**/inventory.html');
    step('login');

    for (const item of ITEMS) await page.click(t(`add-to-cart-${item}`));
    await page.locator(t('shopping-cart-badge')).filter({ hasText: String(ITEMS.length) }).waitFor();
    await page.click(t('shopping-cart-link'));
    await page.waitForURL('**/cart.html');
    await page.click(t('checkout'));
    await page.waitForURL('**/checkout-step-one.html');
    step('cart');

    // Step one: an empty form is refused; then type the buyer and read the fields back.
    await page.click(t('continue'));
    out.empty_form_error = await page.locator(t('error')).innerText();
    await page.fill(t('firstName'), BUYER.firstName);
    await page.fill(t('lastName'), BUYER.lastName);
    await page.fill(t('postalCode'), BUYER.postalCode);
    out.typed_back = { firstName: await page.inputValue(t('firstName')), lastName: await page.inputValue(t('lastName')), postalCode: await page.inputValue(t('postalCode')) };
    await page.click(t('continue'));
    // Either step two loads or the form shows an error: wait for whichever comes first.
    await page.locator(`${t('subtotal-label')}, ${t('error')}`).first().waitFor();
    if (!page.url().includes('checkout-step-two')) {
      out.step_one_error = await page.locator(t('error')).innerText();
      step('step_one');
      return { ...out, steps_ms: steps };
    }
    step('step_one');

    // Step two: the overview's totals against the item prices.
    const prices = await page.locator(t('inventory-item-price')).allInnerTexts();
    out.item_sum = Math.round(prices.reduce((a, p) => a + money(p), 0) * 100) / 100;
    out.subtotal = money(await page.locator(t('subtotal-label')).innerText());
    out.tax = money(await page.locator(t('tax-label')).innerText());
    out.total = money(await page.locator(t('total-label')).innerText());
    out.totals_add_up = out.subtotal === out.item_sum && Math.abs(out.subtotal + out.tax - out.total) < 0.005;
    await page.click(t('finish'));
    await page.waitForURL('**/checkout-complete.html');
    out.completed = await page.locator(t('complete-header')).innerText();
    step('step_two_and_finish');
    return { ...out, steps_ms: steps };
  } finally {
    await context.close();
  }
}

try {
  const runs = [];
  for (const user of USERS) runs.push(await checkout(user));
  console.log(JSON.stringify(runs, null, 2));
} finally {
  await browser.close();
}
