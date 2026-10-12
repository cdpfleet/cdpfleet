// Maven: com.microsoft.playwright:playwright:1.60.0, com.google.code.gson:gson:2.11.0
// Run with PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1. env: CDPFLEET_API_KEY, PROXY_URL
import com.google.gson.*;
import com.microsoft.playwright.*;
import java.net.URI;
import java.net.http.*;
import java.nio.file.*;
import java.util.*;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");
  static final Gson GSON = new GsonBuilder().serializeNulls().setPrettyPrinting().create();
  static final String ORIGIN = "https://httpbin.org";
  // Both files hold a live login: keep them somewhere safe.
  static final Path PLAIN = Path.of(System.getProperty("java.io.tmpdir"), "cdpfleet-state.json");
  static final Path WITH_IDB = Path.of(System.getProperty("java.io.tmpdir"), "cdpfleet-state-idb.json");

  // What a Firebase Auth web app keeps after sign-in: one record in IndexedDB, nothing in
  // cookies or localStorage. Plus a store of values that aren't plain JSON.
  static final String WRITE = """
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
      }""";

  // What the app finds on its next visit.
  static final String READ = """
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
      }""";

  static JsonObject launch() throws Exception {
    String body = "{\"proxy\": " + GSON.toJson(System.getenv("PROXY_URL")) + ", \"headless\": true}";
    HttpResponse<String> res = HttpClient.newHttpClient().send(HttpRequest.newBuilder(URI.create("https://starter.cdpfleet.com/chromium/session"))
        .header("x-api-key", KEY).header("content-type", "application/json")
        .POST(HttpRequest.BodyPublishers.ofString(body)).build(), HttpResponse.BodyHandlers.ofString());
    if (res.statusCode() != 200) throw new RuntimeException("launch: " + res.statusCode() + " " + res.body());
    return JsonParser.parseString(res.body()).getAsJsonObject();
  }

  static boolean cookieSent(Page page) {
    page.navigate(ORIGIN + "/cookies", new Page.NavigateOptions().setTimeout(60000));
    JsonObject cookies = JsonParser.parseString(page.textContent("body")).getAsJsonObject().getAsJsonObject("cookies");
    return cookies.has("session") && cookies.get("session").getAsString().equals("abc123");
  }

  static JsonArray idbDatabases(Path file) throws Exception {
    JsonArray names = new JsonArray();
    for (JsonElement o : JsonParser.parseString(Files.readString(file)).getAsJsonObject().getAsJsonArray("origins")) {
      JsonElement idb = o.getAsJsonObject().get("indexedDB");
      if (idb != null && idb.isJsonArray()) for (JsonElement d : idb.getAsJsonArray()) names.add(d.getAsJsonObject().get("name"));
    }
    return names;
  }

  static JsonObject stateFile(String label, Path file) throws Exception {
    JsonObject f = new JsonObject();
    f.addProperty("state", label);
    f.addProperty("bytes", Files.size(file));
    f.add("indexeddb_databases", idbDatabases(file));
    return f;
  }

  public static void main(String[] args) throws Exception {
    try (Playwright playwright = Playwright.create()) {
      BrowserType.ConnectOptions auth = new BrowserType.ConnectOptions().setHeaders(Map.of("x-api-key", KEY));

      // Session 1: "sign in", then save the state twice: the default way and with IndexedDB.
      JsonObject s1 = launch();
      Browser b1 = playwright.chromium().connect(s1.get("wsUrl").getAsString(), auth);
      try {
        BrowserContext ctx = b1.newContext();
        Page page = ctx.newPage();
        page.navigate(ORIGIN + "/cookies/set?session=abc123", new Page.NavigateOptions().setTimeout(60000));
        page.evaluate("(" + WRITE + ")()");
        ctx.storageState(new BrowserContext.StorageStateOptions().setPath(PLAIN));
        ctx.storageState(new BrowserContext.StorageStateOptions().setPath(WITH_IDB).setIndexedDB(true));
      } finally {
        b1.close();
      }

      // Session 2: a new browser. Restore each file into its own context and look around.
      JsonObject s2 = launch();
      Browser b2 = playwright.chromium().connect(s2.get("wsUrl").getAsString(), auth);
      try {
        JsonArray restored = new JsonArray();
        JsonElement valueTypes = JsonNull.INSTANCE;
        Map<String, Path> files = new LinkedHashMap<>();
        files.put("cookies + localStorage", PLAIN);
        files.put("with IndexedDB", WITH_IDB);
        for (Map.Entry<String, Path> f : files.entrySet()) {
          BrowserContext ctx = b2.newContext(new Browser.NewContextOptions().setStorageStatePath(f.getValue()));
          Page page = ctx.newPage();
          boolean cookie = cookieSent(page);
          JsonObject seen = GSON.toJsonTree(page.evaluate("(" + READ + ")()")).getAsJsonObject();
          JsonObject r = new JsonObject();
          r.addProperty("state", f.getKey());
          r.addProperty("cookie_sent", cookie);
          r.add("local_storage_theme", seen.get("local_storage_theme"));
          r.add("firebase_user", seen.get("firebase_user"));
          r.addProperty("signed_in", !seen.get("firebase_user").isJsonNull());
          restored.add(r);
          if (f.getValue().equals(WITH_IDB)) valueTypes = seen.get("value_types");
          ctx.close();
        }
        JsonObject out = new JsonObject();
        out.addProperty("origin", ORIGIN);
        JsonArray sessions = new JsonArray();
        sessions.add(s1.get("sessionId"));
        sessions.add(s2.get("sessionId"));
        out.add("sessions", sessions);
        JsonArray stateFiles = new JsonArray();
        stateFiles.add(stateFile("cookies + localStorage", PLAIN));
        stateFiles.add(stateFile("with IndexedDB", WITH_IDB));
        out.add("state_files", stateFiles);
        out.add("restored", restored);
        out.add("value_types_after_restore", valueTypes);
        System.out.println(GSON.toJson(out));
      } finally {
        b2.close();
      }
    }
  }
}
