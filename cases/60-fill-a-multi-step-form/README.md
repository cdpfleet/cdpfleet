# A multi-step checkout end to end: wait for each step, and check what you typed

**Easy** · 2026-10-18 · Chromium · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/fill-a-multi-step-form)

> Log in, fill a cart, refuse an empty form, type the buyer, check the totals and finish — as three demo users: one normal, one whose pages are slow, one whose inputs are broken. Waiting for each page instead of sleeping handles the slow one; reading the fields back catches the broken one.

## The problem

Sign-ups, checkouts and quote forms span several pages. A script that clicks through them with fixed sleeps breaks the first time a page is slow, and one that only checks the last page never notices that a field didn't take what it typed. Here the same checkout runs on [Sauce Demo](https://www.saucedemo.com), a public practice shop with demo users built for exactly these problems: `standard_user`, `performance_glitch_user` (slow pages) and `problem_user` (broken inputs). The shop runs entirely in the browser, so there is no server-side order to read back — the checks are on its own pages.

## What we used, and why

| What | Why |
|---|---|
| `page.waitForURL('**/checkout-step-one.html')` | Each step waits for the page it leads to, however long that takes — never a fixed sleep. |
| `locator('[data-test=shopping-cart-badge]').filter({ hasText: '2' }).waitFor()` | Waits until the cart really holds both items before going to it. |
| `locator('[data-test=subtotal-label], [data-test=error]').first().waitFor()` | After submitting a step, waits for whichever comes first: the next step or an error message. |
| `page.inputValue()` | Reads every field back after typing, before submitting. |
| `[data-test=…]` selectors | The shop marks its elements for tests, so the script doesn't depend on layout or wording. |
| A fresh context per user | Each user starts logged out with an empty cart. |

## How it works

1. Log in and wait for the product list.
2. Add two products, wait for the cart badge to show 2, open the cart and start the checkout.
3. Step one: submit the empty form and read the error; type first name, last name and postal code; read the three fields back; submit.
4. Step two: compare the subtotal with the sum of the item prices and the total with subtotal + tax; finish and read the confirmation.
5. The same for each of the three users, timing every step.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-10-18):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY`, `PROXY_URL`, `PROXY_URL_US` (see [cases/README.md](../README.md#environment)).

## What we got

| User | Login (ms) | Cart (ms) | Fields read back | Result | Totals |
|---|---|---|---|---|---|
| standard_user | 2674 | 535 | Ada / Lovelace / 10115 | Thank you for your order! | 39.98 + 3.2 = 43.18 ✓ |
| performance_glitch_user | 8004 | 292 | Ada / Lovelace / 10115 | Thank you for your order! | 39.98 + 3.2 = 43.18 ✓ |
| problem_user | 1960 | 280 | Lovelace / ∅ / 10115 | Error: Last Name is required | — |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **Waiting for the next page instead of sleeping handles slow steps.** `performance_glitch_user` lost about five seconds per slow step in every run — on the login, on reaching the cart, or both — where `standard_user` logged in within two or three; the same script finished both, because each step waits for its page rather than for a fixed time. A one-second sleep would have clicked into a page that wasn't there yet.
- **Read back what you typed.** For `problem_user`, the last name went into the first-name field and the last name stayed empty — the script typed everything correctly, and the form still refused it with "Last Name is required". Reading the fields back before submitting shows where it went wrong instead of a vague failure later.
- **Check the numbers on the confirmation steps.** The overview's subtotal matched the sum of the two item prices and the total matched subtotal + tax; both completed checkouts ended with "Thank you for your order!".
- An empty step-one form was refused with "Error: First Name is required" for every user: wait for the error element, not for a timeout, to learn a step didn't go through.
