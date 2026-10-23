// dotnet add package Microsoft.Playwright --version 1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
using System.Net.Http.Json;
using System.Security.Cryptography;
using System.Text;
using System.Text.Encodings.Web;
using System.Text.Json;
using System.Text.Json.Nodes;
using Microsoft.Playwright;

var key = Environment.GetEnvironmentVariable("CDPFLEET_API_KEY")!;
var run = Convert.ToHexString(RandomNumberGenerator.GetBytes(3)).ToLower();
var engines = new (string Engine, string Family)[] { ("chrome", "chromium"), ("edge", "chromium"), ("chromium", "chromium"), ("brave", "chromium"),
    ("camoufox", "firefox"), ("firefox", "firefox"), ("webkit", "webkit") };
const int Launches = 3;
// Brave and Camoufox key their noise to the site, so the script runs on a real page.
const string Site = "https://example.com/";

// What a fingerprinting script computes: canvas, WebGL (renderer + a rendered image), audio and a
// font list, each as a short hash; canvas and audio are drawn twice to see whether a session is
// consistent with itself. The same script runs in every language.
const string Fp = @"(async () => {
  const fnv = (s) => { let h = 0x811c9dc5; for (let i = 0; i < s.length; i++) { h ^= s.charCodeAt(i); h = Math.imul(h, 0x01000193) >>> 0; } return h.toString(16).padStart(8, '0'); };
  const draw = () => {
    const c = document.createElement('canvas'); c.width = 240; c.height = 60;
    const x = c.getContext('2d'); x.textBaseline = 'top'; x.font = '16px Arial';
    x.fillStyle = '#f60'; x.fillRect(100, 1, 62, 20);
    x.fillStyle = '#069'; x.fillText('cdpfleet fp 66 <@>', 2, 15);
    x.fillStyle = 'rgba(102, 204, 0, 0.7)'; x.fillText('cdpfleet fp 66 <@>', 4, 17);
    return c.toDataURL();
  };
  const webgl = () => {
    try {
      const c = document.createElement('canvas'); c.width = 64; c.height = 64;
      const gl = c.getContext('webgl', { preserveDrawingBuffer: true });
      if (!gl) return { renderer: null, image: null };
      const dbg = gl.getExtension('WEBGL_debug_renderer_info');
      const renderer = dbg ? gl.getParameter(dbg.UNMASKED_RENDERER_WEBGL) : gl.getParameter(gl.RENDERER);
      const sh = (type, src) => { const s = gl.createShader(type); gl.shaderSource(s, src); gl.compileShader(s); return s; };
      const pr = gl.createProgram();
      gl.attachShader(pr, sh(gl.VERTEX_SHADER, 'attribute vec2 p;varying vec2 v;void main(){v=p;gl_Position=vec4(p,0.0,1.0);}'));
      gl.attachShader(pr, sh(gl.FRAGMENT_SHADER, 'precision mediump float;varying vec2 v;void main(){gl_FragColor=vec4(sin(v.x*7.1)*0.5+0.5,cos(v.y*5.3)*0.5+0.5,fract(v.x*v.y*13.7),1.0);}'));
      gl.linkProgram(pr); gl.useProgram(pr);
      gl.bindBuffer(gl.ARRAY_BUFFER, gl.createBuffer());
      gl.bufferData(gl.ARRAY_BUFFER, new Float32Array([-1, -1, 1, -1, 0, 1]), gl.STATIC_DRAW);
      const loc = gl.getAttribLocation(pr, 'p'); gl.enableVertexAttribArray(loc); gl.vertexAttribPointer(loc, 2, gl.FLOAT, false, 0, 0);
      gl.drawArrays(gl.TRIANGLES, 0, 3);
      const px = new Uint8Array(64 * 64 * 4); gl.readPixels(0, 0, 64, 64, gl.RGBA, gl.UNSIGNED_BYTE, px);
      return { renderer, image: fnv(px.join(',')) };
    } catch (e) { return { renderer: null, image: null }; }
  };
  const audio = async () => {
    try {
      const ctx = new OfflineAudioContext(1, 44100, 44100);
      const o = ctx.createOscillator(); o.type = 'triangle'; o.frequency.value = 10000;
      const k = ctx.createDynamicsCompressor();
      k.threshold.value = -50; k.knee.value = 40; k.ratio.value = 12; k.attack.value = 0; k.release.value = 0.25;
      o.connect(k); k.connect(ctx.destination); o.start(0);
      // copyFromChannel into our own array: Camoufox runs this script in an isolated world, where
      // reading the page's audio buffer directly is refused.
      const d = new Float32Array(500); (await ctx.startRendering()).copyFromChannel(d, 0, 4500);
      let s = ''; for (let i = 0; i < 500; i++) s += d[i].toFixed(10) + ',';
      return fnv(s);
    } catch (e) { return 'error: ' + e.name; }
  };
  // Width probe: a family counts as installed when it measures differently from all three fallbacks.
  const FAMILIES = ['Arial', 'Arial Black', 'Calibri', 'Cambria', 'Candara', 'Comic Sans MS', 'Consolas', 'Courier New', 'DejaVu Sans', 'Georgia', 'Helvetica', 'Helvetica Neue', 'Impact', 'Liberation Sans', 'Lucida Console', 'Lucida Grande', 'Menlo', 'Monaco', 'Noto Sans', 'Palatino', 'Segoe UI', 'Tahoma', 'Times New Roman', 'Trebuchet MS', 'Ubuntu', 'Verdana', 'Roboto', 'Open Sans', 'Fira Code', 'Avenir'];
  const fonts = () => {
    const c = document.createElement('canvas').getContext('2d');
    const w = (f) => { c.font = '48px ' + f; return c.measureText('mmmmmmmmmmlli1WQ@#').width; };
    const base = ['monospace', 'serif', 'sans-serif'].map((b) => [b, w(b)]);
    return FAMILIES.filter((f) => base.some(([b, bw]) => w(JSON.stringify(f) + ', ' + b) !== bw));
  };
  const c1 = draw(), c2 = draw();
  const a1 = await audio(), a2 = await audio();
  const g = webgl();
  const found = fonts();
  return {
    canvas: fnv(c1), canvas_same_in_session: c1 === c2,
    webgl_renderer: g.renderer, webgl_image: g.image,
    audio: a1, audio_same_in_session: a1 === a2,
    fonts: fnv(found.join(',')), font_count: found.length,
    user_agent: navigator.userAgent,
  };
})()";

using var http = new HttpClient();
http.DefaultRequestHeaders.Add("x-api-key", key);
using var playwright = await Playwright.CreateAsync();

// The node saves a profile after the browser exits: wait for the new version before relaunching.
async Task WaitForVersion(string name, int version)
{
    for (var i = 0; i < 60; i++)
    {
        var profiles = (await http.GetFromJsonAsync<JsonElement>("https://cdpfleet.com/v1/me/profiles")).GetProperty("profiles");
        foreach (var p in profiles.EnumerateArray())
            if (p.GetProperty("name").GetString() == name && p.GetProperty("version").GetInt32() >= version && !p.GetProperty("in_use").GetBoolean()) return;
        await Task.Delay(2000);
    }
    throw new Exception($"profile {name} never reached version {version}");
}

// 503 = the fleet is momentarily busy: wait Retry-After and try again. 409 profile_version_downgrade =
// the profile was saved by a newer build than this server has (mid-rollout): another try lands elsewhere.
async Task<JsonElement> Launch(string engine, JsonObject body)
{
    for (var attempt = 1; ; attempt++)
    {
        var res = await http.PostAsync($"https://starter.cdpfleet.com/{engine}/session", new StringContent(body.ToJsonString(), Encoding.UTF8, "application/json"));
        if (res.IsSuccessStatusCode) return await res.Content.ReadFromJsonAsync<JsonElement>();
        var text = await res.Content.ReadAsStringAsync();
        var retry = (int)res.StatusCode == 503 || ((int)res.StatusCode == 409 && text.Contains("profile_version_downgrade"));
        if (!retry || attempt == 5) throw new Exception($"{engine} launch {(int)res.StatusCode} {text}");
        await Task.Delay((int)(res.Headers.RetryAfter?.Delta?.TotalMilliseconds ?? 5000));
    }
}

// Proxies drop a tunnel now and then: up to two retries.
async Task Go(IPage page)
{
    for (var attempt = 1; ; attempt++)
    {
        try { await page.GotoAsync(Site, new() { Timeout = 60000 }); return; }
        catch (PlaywrightException) when (attempt < 3) { await Task.Delay(2000); }
    }
}

async Task<JsonNode> Measure(string engine, string family, string? profile)
{
    var body = new JsonObject { ["proxy"] = Environment.GetEnvironmentVariable("PROXY_URL"), ["headless"] = true, ["record"] = false };
    if (profile != null) body["profile"] = profile;
    var s = await Launch(engine, body);
    var type = family == "firefox" ? playwright.Firefox : family == "webkit" ? playwright.Webkit : playwright.Chromium;
    var browser = await type.ConnectAsync(s.GetProperty("wsUrl").GetString()!, new() { Headers = new Dictionary<string, string> { ["x-api-key"] = key } });
    try
    {
        // A profile lives in browser.Contexts[0]; without one, a fresh context.
        var context = profile != null ? browser.Contexts[0] : await browser.NewContextAsync();
        var page = context.Pages.Count > 0 ? context.Pages[0] : await context.NewPageAsync();
        await Go(page);
        return JsonNode.Parse((await page.EvaluateAsync<JsonElement>(Fp)).GetRawText())!;
    }
    finally
    {
        await browser.CloseAsync();
    }
}

var launches = new JsonObject();
var profiles = new JsonObject();
foreach (var (engine, family) in engines)
{
    var runs = new JsonArray();
    for (var i = 0; i < Launches; i++) runs.Add(await Measure(engine, family, null));
    launches[engine] = runs;
    // Two launches with one new profile: what does it keep?
    var name = $"case66-{engine}-{run}";
    try
    {
        var first = await Measure(engine, family, name);
        await WaitForVersion(name, 1);
        var second = await Measure(engine, family, name);
        await WaitForVersion(name, 2);
        profiles[engine] = new JsonArray(first, second);
    }
    finally
    {
        await http.DeleteAsync($"https://cdpfleet.com/v1/me/profiles/{name}");
    }
}
Console.WriteLine(new JsonObject { ["launches"] = launches, ["profile"] = profiles }.ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));
