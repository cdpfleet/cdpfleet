// go get github.com/playwright-community/playwright-go@v0.6000.0
// Driver: build playwright-core 1.60.0 from npm and set PLAYWRIGHT_DRIVER_PATH (see /docs/quickstart).
// env: CDPFLEET_API_KEY, PROXY_URL_US
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/playwright-community/playwright-go"
)

var key = os.Getenv("CDPFLEET_API_KEY")

const site = "https://www.saucedemo.com" // a public demo shop for testing practice
const password = "secret_sauce"          // printed on the shop's login page

// The shop's demo users: a normal one, one whose pages are slow, and one with broken inputs.
var users = []string{"standard_user", "performance_glitch_user", "problem_user"}
var buyer = [][2]string{{"firstName", "Ada"}, {"lastName", "Lovelace"}, {"postalCode", "10115"}}
var items = []string{"sauce-labs-backpack", "sauce-labs-bike-light"}

func t(sel string) string { return `[data-test="` + sel + `"]` }

var notMoney = regexp.MustCompile(`[^0-9.]`)

func money(s string) float64 {
	v, _ := strconv.ParseFloat(notMoney.ReplaceAllString(s, ""), 64)
	return v
}

func launch(name string, options map[string]any) (map[string]any, error) {
	body, _ := json.Marshal(options)
	req, _ := http.NewRequest("POST", "https://starter.cdpfleet.com/"+name+"/session", bytes.NewReader(body))
	req.Header.Set("x-api-key", key)
	req.Header.Set("content-type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(res.Body)
		return nil, fmt.Errorf("launch %s: %s %s", name, res.Status, msg)
	}
	var session map[string]any
	return session, json.NewDecoder(res.Body).Decode(&session)
}

type run struct {
	User           string            `json:"user"`
	EmptyFormError *string           `json:"empty_form_error"`
	TypedBack      map[string]string `json:"typed_back"`
	StepOneError   *string           `json:"step_one_error"`
	ItemSum        *float64          `json:"item_sum"`
	Subtotal       *float64          `json:"subtotal"`
	Tax            *float64          `json:"tax"`
	Total          *float64          `json:"total"`
	TotalsAddUp    *bool             `json:"totals_add_up"`
	Completed      *string           `json:"completed"`
	StepsMs        map[string]int64  `json:"steps_ms"`
}

func must[T any](v T, err error) T {
	if err != nil {
		log.Fatal(err)
	}
	return v
}

func check(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

// The checkout as one user, in a fresh context: every step waits for the page it leads to
// (WaitForURL), never for a fixed time.
func checkout(browser playwright.Browser, user string) run {
	context := must(browser.NewContext())
	defer context.Close()
	page := must(context.NewPage())
	out := run{User: user, StepsMs: map[string]int64{}}
	mark := time.Now()
	step := func(name string) { out.StepsMs[name] = time.Since(mark).Milliseconds(); mark = time.Now() }

	_, err := page.Goto(site)
	check(err)
	check(page.Locator(t("username")).Fill(user))
	check(page.Locator(t("password")).Fill(password))
	check(page.Locator(t("login-button")).Click())
	check(page.WaitForURL("**/inventory.html"))
	step("login")

	for _, item := range items {
		check(page.Locator(t("add-to-cart-" + item)).Click())
	}
	check(page.Locator(t("shopping-cart-badge")).Filter(playwright.LocatorFilterOptions{HasText: strconv.Itoa(len(items))}).WaitFor())
	check(page.Locator(t("shopping-cart-link")).Click())
	check(page.WaitForURL("**/cart.html"))
	check(page.Locator(t("checkout")).Click())
	check(page.WaitForURL("**/checkout-step-one.html"))
	step("cart")

	// Step one: an empty form is refused; then type the buyer and read the fields back.
	check(page.Locator(t("continue")).Click())
	emptyErr := must(page.Locator(t("error")).InnerText())
	out.EmptyFormError = &emptyErr
	for _, f := range buyer {
		check(page.Locator(t(f[0])).Fill(f[1]))
	}
	out.TypedBack = map[string]string{}
	for _, f := range buyer {
		out.TypedBack[f[0]] = must(page.Locator(t(f[0])).InputValue())
	}
	check(page.Locator(t("continue")).Click())
	// Either step two loads or the form shows an error: wait for whichever comes first.
	check(page.Locator(t("subtotal-label") + ", " + t("error")).First().WaitFor())
	if !strings.Contains(page.URL(), "checkout-step-two") {
		stepErr := must(page.Locator(t("error")).InnerText())
		out.StepOneError = &stepErr
		step("step_one")
		return out
	}
	step("step_one")

	// Step two: the overview's totals against the item prices.
	sum := 0.0
	for _, p := range must(page.Locator(t("inventory-item-price")).AllInnerTexts()) {
		sum += money(p)
	}
	itemSum := math.Round(sum*100) / 100
	subtotal := money(must(page.Locator(t("subtotal-label")).InnerText()))
	tax := money(must(page.Locator(t("tax-label")).InnerText()))
	total := money(must(page.Locator(t("total-label")).InnerText()))
	addsUp := subtotal == itemSum && math.Abs(subtotal+tax-total) < 0.005
	out.ItemSum, out.Subtotal, out.Tax, out.Total, out.TotalsAddUp = &itemSum, &subtotal, &tax, &total, &addsUp
	check(page.Locator(t("finish")).Click())
	check(page.WaitForURL("**/checkout-complete.html"))
	done := must(page.Locator(t("complete-header")).InnerText())
	out.Completed = &done
	step("step_two_and_finish")
	return out
}

func main() {
	session := must(launch("chromium", map[string]any{"proxy": os.Getenv("PROXY_URL_US"), "headless": true}))
	pw := must(playwright.Run(&playwright.RunOptions{SkipInstallBrowsers: true}))
	defer pw.Stop()
	browser := must(pw.Chromium.Connect(session["wsUrl"].(string), playwright.BrowserTypeConnectOptions{Headers: map[string]string{"x-api-key": key}}))
	defer browser.Close()
	runs := []run{}
	for _, user := range users {
		runs = append(runs, checkout(browser, user))
	}
	out, _ := json.MarshalIndent(runs, "", "  ")
	fmt.Println(string(out))
}
