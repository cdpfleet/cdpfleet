// go get github.com/playwright-community/playwright-go@v0.6000.0
// Driver: build playwright-core 1.60.0 from npm and set PLAYWRIGHT_DRIVER_PATH (see /docs/quickstart).
// env: CDPFLEET_API_KEY, PROXY_URL
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/playwright-community/playwright-go"
)

const origin = "https://httpbin.org"

var (
	key = os.Getenv("CDPFLEET_API_KEY")
	// Both files hold a live login: keep them somewhere safe.
	plain   = filepath.Join(os.TempDir(), "cdpfleet-state.json")
	withIdb = filepath.Join(os.TempDir(), "cdpfleet-state-idb.json")
)

// What a Firebase Auth web app keeps after sign-in: one record in IndexedDB, nothing in
// cookies or localStorage. Plus a store of values that aren't plain JSON.
const write = `async () => {
  const open = (name, setup) => new Promise((ok, no) => {
    const r = indexedDB.open(name, 1);
    r.onupgradeneeded = () => setup(r.result);
    r.onsuccess = () => ok(r.result);
    r.onerror = () => no(r.error);
  });
  const put = (db, store, value, key) => new Promise((ok, no) => {
    const tx = db.transaction(store, 'readwrite');
    tx.objectStore(store).put(value, key);
    tx.oncomplete = ok;
    tx.onerror = () => no(tx.error);
  });
  const fb = await open('firebaseLocalStorageDb', (db) => db.createObjectStore('firebaseLocalStorage', { keyPath: 'fbase_key' }));
  await put(fb, 'firebaseLocalStorage', {
    fbase_key: 'firebase:authUser:AIzaDemoKey:[DEFAULT]',
    value: { uid: 'u_8f3a', email: 'alice@example.com', stsTokenManager: { refreshToken: 'demo-refresh-token', accessToken: 'demo-access-token', expirationTime: Date.now() + 3600000 } },
  });
  const types = await open('typesDb', (db) => db.createObjectStore('values'));
  const values = { date: new Date(0), bytes: new Uint8Array([1, 2, 3]), bigint: 10n, map: new Map([['k', 1]]), blob: new Blob(['hi']) };
  for (const [k, v] of Object.entries(values)) await put(types, 'values', v, k);
  localStorage.setItem('theme', 'dark');
}`

// What the app finds on its next visit.
const read = `async () => {
  const dbs = (await indexedDB.databases()).map((d) => d.name);
  const get = (name, store, key) => new Promise((ok) => {
    if (!dbs.includes(name)) return ok(undefined);
    const r = indexedDB.open(name);
    r.onsuccess = () => {
      const q = r.result.transaction(store).objectStore(store).get(key);
      q.onsuccess = () => ok(q.result);
      q.onerror = () => ok(undefined);
    };
    r.onerror = () => ok(undefined);
  });
  const user = await get('firebaseLocalStorageDb', 'firebaseLocalStorage', 'firebase:authUser:AIzaDemoKey:[DEFAULT]');
  const types = {};
  for (const k of ['date', 'bytes', 'bigint', 'map', 'blob']) {
    const v = await get('typesDb', 'values', k);
    const t = v === undefined ? 'missing' : typeof v === 'bigint' ? 'BigInt' : Object.prototype.toString.call(v).slice(8, -1);
    types[k] = t === 'Object' && Object.keys(v).length === 0 ? 'Object {} (emptied)' : t;
  }
  return { local_storage_theme: localStorage.getItem('theme'), firebase_user: user ? user.value.email : null, value_types: types };
}`

func launch() (map[string]any, error) {
	body, _ := json.Marshal(map[string]any{"proxy": os.Getenv("PROXY_URL"), "headless": true})
	req, _ := http.NewRequest("POST", "https://starter.cdpfleet.com/chromium/session", bytes.NewReader(body))
	req.Header.Set("x-api-key", key)
	req.Header.Set("content-type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(res.Body)
		return nil, fmt.Errorf("launch: %s %s", res.Status, msg)
	}
	var session map[string]any
	return session, json.NewDecoder(res.Body).Decode(&session)
}

type stateFile struct {
	Origins []struct {
		Origin    string           `json:"origin"`
		IndexedDB []map[string]any `json:"indexedDB"`
	} `json:"origins"`
}

func readState(file string) stateFile {
	var s stateFile
	data, err := os.ReadFile(file)
	if err != nil {
		log.Fatal(err)
	}
	if err := json.Unmarshal(data, &s); err != nil {
		log.Fatal(err)
	}
	return s
}

func idbDatabases(file string) []string {
	names := []string{}
	for _, o := range readState(file).Origins {
		for _, d := range o.IndexedDB {
			names = append(names, d["name"].(string))
		}
	}
	return names
}

func cookieSent(page playwright.Page) bool {
	res, err := page.Goto(origin+"/cookies", playwright.PageGotoOptions{Timeout: playwright.Float(60000)})
	if err != nil {
		log.Fatal(err)
	}
	var body struct {
		Cookies map[string]string `json:"cookies"`
	}
	if err := res.JSON(&body); err != nil {
		log.Fatal(err)
	}
	return body.Cookies["session"] == "abc123"
}

type restoredState struct {
	State             string `json:"state"`
	CookieSent        bool   `json:"cookie_sent"`
	LocalStorageTheme any    `json:"local_storage_theme"`
	FirebaseUser      any    `json:"firebase_user"`
	SignedIn          bool   `json:"signed_in"`
}

type fileInfo struct {
	State              string   `json:"state"`
	Bytes              int64    `json:"bytes"`
	IndexedDBDatabases []string `json:"indexeddb_databases"`
}

func size(file string) int64 {
	st, err := os.Stat(file)
	if err != nil {
		log.Fatal(err)
	}
	return st.Size()
}

// Session 1: set a cookie, write the Firebase-shaped login, save both state files.
func signIn(pw *playwright.Playwright, auth playwright.BrowserTypeConnectOptions) map[string]any {
	s1, err := launch()
	if err != nil {
		log.Fatal(err)
	}
	b1, err := pw.Chromium.Connect(s1["wsUrl"].(string), auth)
	if err != nil {
		log.Fatal(err)
	}
	// log.Panic below (not Fatal) so this deferred Close still ends the fleet session.
	defer b1.Close()
	ctx, err := b1.NewContext()
	if err != nil {
		log.Panic(err)
	}
	page, err := ctx.NewPage()
	if err != nil {
		log.Panic(err)
	}
	if _, err := page.Goto(origin+"/cookies/set?session=abc123", playwright.PageGotoOptions{Timeout: playwright.Float(60000)}); err != nil {
		log.Panic(err)
	}
	if _, err := page.Evaluate(write); err != nil {
		log.Panic(err)
	}
	if _, err := ctx.StorageState(playwright.BrowserContextStorageStateOptions{Path: playwright.String(plain)}); err != nil {
		log.Panic(err)
	}
	if _, err := ctx.StorageState(playwright.BrowserContextStorageStateOptions{Path: playwright.String(withIdb), IndexedDB: playwright.Bool(true)}); err != nil {
		log.Panic(err)
	}
	return s1
}

func main() {
	pw, err := playwright.Run(&playwright.RunOptions{SkipInstallBrowsers: true})
	if err != nil {
		log.Fatal(err)
	}
	defer pw.Stop()
	auth := playwright.BrowserTypeConnectOptions{Headers: map[string]string{"x-api-key": key}}

	// Session 1: "sign in", then save the state twice: the default way and with IndexedDB.
	s1 := signIn(pw, auth)

	// Session 2: a new browser. Restore each file into its own context and look around.
	s2, err := launch()
	if err != nil {
		log.Fatal(err)
	}
	b2, err := pw.Chromium.Connect(s2["wsUrl"].(string), auth)
	if err != nil {
		log.Fatal(err)
	}
	defer b2.Close()
	restored := []restoredState{}
	var valueTypes any
	for _, f := range []struct{ label, file string }{{"cookies + localStorage", plain}, {"with IndexedDB", withIdb}} {
		// Not NewContext's StorageStatePath: playwright-go parses that file into a typed
		// struct with no IndexedDB field and silently drops it. SetStorageState passes the
		// file to Playwright untouched.
		ctx, err := b2.NewContext()
		if err != nil {
			log.Fatal(err)
		}
		if err := ctx.SetStorageState(f.file); err != nil {
			log.Fatal(err)
		}
		page, err := ctx.NewPage()
		if err != nil {
			log.Fatal(err)
		}
		cookie := cookieSent(page)
		v, err := page.Evaluate(read)
		if err != nil {
			log.Fatal(err)
		}
		seen := v.(map[string]any)
		restored = append(restored, restoredState{
			State: f.label, CookieSent: cookie, LocalStorageTheme: seen["local_storage_theme"],
			FirebaseUser: seen["firebase_user"], SignedIn: seen["firebase_user"] != nil,
		})
		if f.file == withIdb {
			valueTypes = seen["value_types"]
		}
		ctx.Close()
	}
	out, _ := json.MarshalIndent(map[string]any{
		"origin":   origin,
		"sessions": []any{s1["sessionId"], s2["sessionId"]},
		"state_files": []fileInfo{
			{"cookies + localStorage", size(plain), idbDatabases(plain)},
			{"with IndexedDB", size(withIdb), idbDatabases(withIdb)},
		},
		"restored":                  restored,
		"value_types_after_restore": valueTypes,
	}, "", "  ")
	fmt.Println(string(out))
}
