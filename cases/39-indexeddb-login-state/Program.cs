// dotnet add package Microsoft.Playwright --version 1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
using System.Net.Http.Json;
using System.Text.Encodings.Web;
using System.Text.Json;
using System.Text.Json.Nodes;
using Microsoft.Playwright;

const string Origin = "https://httpbin.org";
// Both files hold a live login: keep them somewhere safe.
var plain = Path.Combine(Path.GetTempPath(), "cdpfleet-state.json");
var withIdb = Path.Combine(Path.GetTempPath(), "cdpfleet-state-idb.json");

// What a Firebase Auth web app keeps after sign-in: one record in IndexedDB, nothing in
// cookies or localStorage. Plus a store of values that aren't plain JSON.
const string Write = """
    async () => {
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
    }
    """;

// What the app finds on its next visit.
const string Read = """
    async () => {
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
    }
    """;

var key = Environment.GetEnvironmentVariable("CDPFLEET_API_KEY")!;
using var http = new HttpClient();
http.DefaultRequestHeaders.Add("x-api-key", key);
async Task<JsonElement> Launch()
{
    var res = await http.PostAsJsonAsync("https://starter.cdpfleet.com/chromium/session",
        new { proxy = Environment.GetEnvironmentVariable("PROXY_URL"), headless = true });
    if (!res.IsSuccessStatusCode) throw new Exception($"launch: {(int)res.StatusCode} {await res.Content.ReadAsStringAsync()}");
    return await res.Content.ReadFromJsonAsync<JsonElement>();
}

async Task<bool> CookieSent(IPage page)
{
    await page.GotoAsync($"{Origin}/cookies", new() { Timeout = 60000 });
    var cookies = JsonNode.Parse(await page.TextContentAsync("body") ?? "{}")!["cookies"]!.AsObject();
    return cookies["session"]?.GetValue<string>() == "abc123";
}

JsonArray IdbDatabases(string file)
{
    var names = new JsonArray();
    foreach (var o in JsonNode.Parse(File.ReadAllText(file))!["origins"]!.AsArray())
        if (o!["indexedDB"] is JsonArray dbs)
            foreach (var d in dbs) names.Add(d!["name"]!.GetValue<string>());
    return names;
}

using var playwright = await Playwright.CreateAsync();
var auth = new BrowserTypeConnectOptions { Headers = new Dictionary<string, string> { ["x-api-key"] = key } };

// Session 1: "sign in", then save the state twice: the default way and with IndexedDB.
var s1 = await Launch();
var b1 = await playwright.Chromium.ConnectAsync(s1.GetProperty("wsUrl").GetString()!, auth);
try
{
    var ctx = await b1.NewContextAsync();
    var page = await ctx.NewPageAsync();
    await page.GotoAsync($"{Origin}/cookies/set?session=abc123", new() { Timeout = 60000 });
    await page.EvaluateAsync(Write);
    await ctx.StorageStateAsync(new() { Path = plain });
    await ctx.StorageStateAsync(new() { Path = withIdb, IndexedDB = true });
}
finally
{
    await b1.CloseAsync();
}

// Session 2: a new browser. Restore each file into its own context and look around.
var s2 = await Launch();
var b2 = await playwright.Chromium.ConnectAsync(s2.GetProperty("wsUrl").GetString()!, auth);
try
{
    var restored = new JsonArray();
    JsonNode? valueTypes = null;
    foreach (var (label, file) in new[] { ("cookies + localStorage", plain), ("with IndexedDB", withIdb) })
    {
        var ctx = await b2.NewContextAsync(new() { StorageStatePath = file });
        var page = await ctx.NewPageAsync();
        var cookie = await CookieSent(page);
        var seen = JsonNode.Parse((await page.EvaluateAsync<JsonElement>(Read)).GetRawText())!;
        var user = seen["firebase_user"]?.GetValue<string>();
        restored.Add(new JsonObject
        {
            ["state"] = label,
            ["cookie_sent"] = cookie,
            ["local_storage_theme"] = seen["local_storage_theme"]?.GetValue<string>(),
            ["firebase_user"] = user,
            ["signed_in"] = user != null,
        });
        if (file == withIdb) valueTypes = seen["value_types"]?.DeepClone();
        await ctx.CloseAsync();
    }
    var output = new JsonObject
    {
        ["origin"] = Origin,
        ["sessions"] = new JsonArray(s1.GetProperty("sessionId").GetString(), s2.GetProperty("sessionId").GetString()),
        ["state_files"] = new JsonArray(
            new JsonObject { ["state"] = "cookies + localStorage", ["bytes"] = new FileInfo(plain).Length, ["indexeddb_databases"] = IdbDatabases(plain) },
            new JsonObject { ["state"] = "with IndexedDB", ["bytes"] = new FileInfo(withIdb).Length, ["indexeddb_databases"] = IdbDatabases(withIdb) }),
        ["restored"] = restored,
        ["value_types_after_restore"] = valueTypes,
    };
    Console.WriteLine(output.ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));
}
finally
{
    await b2.CloseAsync();
}
