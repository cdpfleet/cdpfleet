// go get github.com/playwright-community/playwright-go@v0.6000.0
// Driver: build playwright-core 1.60.0 from npm and set PLAYWRIGHT_DRIVER_PATH (see /docs/quickstart).
// env: CDPFLEET_API_KEY, PROXY_URL
package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/playwright-community/playwright-go"
)

var key = os.Getenv("CDPFLEET_API_KEY")

var engines = [][2]string{{"chrome", "chromium"}, {"edge", "chromium"}, {"chromium", "chromium"}, {"brave", "chromium"},
	{"camoufox", "firefox"}, {"firefox", "firefox"}, {"webkit", "webkit"}}

const launches = 3

// Brave and Camoufox key their noise to the site, so the script runs on a real page.
const site = "https://example.com/"

// What a fingerprinting script computes: canvas, WebGL (renderer + a rendered image), audio and a
// font list, each as a short hash; canvas and audio are drawn twice to see whether a session is
// consistent with itself. The same script runs in every language.
const fp = `(async () => {
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
})()`

// Field order matches the other languages' output.
type measurement struct {
	Canvas              string  `json:"canvas"`
	CanvasSameInSession bool    `json:"canvas_same_in_session"`
	WebGLRenderer       *string `json:"webgl_renderer"`
	WebGLImage          *string `json:"webgl_image"`
	Audio               *string `json:"audio"`
	AudioSameInSession  bool    `json:"audio_same_in_session"`
	Fonts               string  `json:"fonts"`
	FontCount           int     `json:"font_count"`
	UserAgent           string  `json:"user_agent"`
}

func api(method, path string) (*http.Response, error) {
	req, _ := http.NewRequest(method, "https://cdpfleet.com"+path, nil)
	req.Header.Set("x-api-key", key)
	return http.DefaultClient.Do(req)
}

// The node saves a profile after the browser exits: wait for the new version before relaunching.
func waitForVersion(name string, version int) error {
	for i := 0; i < 60; i++ {
		res, err := api("GET", "/v1/me/profiles")
		if err == nil {
			var body struct {
				Profiles []struct {
					Name    string `json:"name"`
					Version int    `json:"version"`
					InUse   bool   `json:"in_use"`
				} `json:"profiles"`
			}
			json.NewDecoder(res.Body).Decode(&body)
			res.Body.Close()
			for _, p := range body.Profiles {
				if p.Name == name && p.Version >= version && !p.InUse {
					return nil
				}
			}
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("profile %s never reached version %d", name, version)
}

// 503 = the fleet is momentarily busy: wait Retry-After and try again. 409 profile_version_downgrade =
// the profile was saved by a newer build than this server has (mid-rollout): another try lands elsewhere.
func launch(engine string, opts map[string]any) (map[string]any, error) {
	body, _ := json.Marshal(opts)
	for attempt := 1; ; attempt++ {
		req, _ := http.NewRequest("POST", "https://starter.cdpfleet.com/"+engine+"/session", bytes.NewReader(body))
		req.Header.Set("x-api-key", key)
		req.Header.Set("content-type", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode == 200 {
			var session map[string]any
			return session, json.Unmarshal(b, &session)
		}
		retry := res.StatusCode == 503 || (res.StatusCode == 409 && strings.Contains(string(b), "profile_version_downgrade"))
		if !retry || attempt == 5 {
			return nil, fmt.Errorf("%s launch %d %s", engine, res.StatusCode, strings.TrimSpace(string(b)))
		}
		wait, err := strconv.Atoi(res.Header.Get("retry-after"))
		if err != nil {
			wait = 5
		}
		time.Sleep(time.Duration(wait) * time.Second)
	}
}

// Proxies drop a tunnel now and then: up to two retries.
func goTo(page playwright.Page) error {
	for attempt := 1; ; attempt++ {
		_, err := page.Goto(site, playwright.PageGotoOptions{Timeout: playwright.Float(60000)})
		if err == nil || attempt == 3 {
			return err
		}
		time.Sleep(2 * time.Second)
	}
}

func measure(pw *playwright.Playwright, engine, family, profile string) (measurement, error) {
	opts := map[string]any{"proxy": os.Getenv("PROXY_URL"), "headless": true, "record": false}
	if profile != "" {
		opts["profile"] = profile
	}
	s, err := launch(engine, opts)
	if err != nil {
		return measurement{}, err
	}
	bt := pw.Chromium
	if family == "firefox" {
		bt = pw.Firefox
	} else if family == "webkit" {
		bt = pw.WebKit
	}
	browser, err := bt.Connect(s["wsUrl"].(string), playwright.BrowserTypeConnectOptions{Headers: map[string]string{"x-api-key": key}})
	if err != nil {
		return measurement{}, err
	}
	defer browser.Close()
	// A profile lives in browser.Contexts()[0]; without one, a fresh context.
	var context playwright.BrowserContext
	if profile != "" {
		context = browser.Contexts()[0]
	} else if context, err = browser.NewContext(); err != nil {
		return measurement{}, err
	}
	var page playwright.Page
	if pages := context.Pages(); len(pages) > 0 {
		page = pages[0]
	} else if page, err = context.NewPage(); err != nil {
		return measurement{}, err
	}
	if err := goTo(page); err != nil {
		return measurement{}, err
	}
	raw, err := page.Evaluate(fp)
	if err != nil {
		return measurement{}, err
	}
	b, _ := json.Marshal(raw)
	var m measurement
	return m, json.Unmarshal(b, &m)
}

func run() error {
	rnd := make([]byte, 3)
	rand.Read(rnd)
	runID := hex.EncodeToString(rnd)
	pw, err := playwright.Run(&playwright.RunOptions{SkipInstallBrowsers: true})
	if err != nil {
		return err
	}
	defer pw.Stop()
	all := map[string][]measurement{}
	profiles := map[string][]measurement{}
	for _, e := range engines {
		for i := 0; i < launches; i++ {
			m, err := measure(pw, e[0], e[1], "")
			if err != nil {
				return err
			}
			all[e[0]] = append(all[e[0]], m)
		}
		// Two launches with one new profile: what does it keep?
		name := "case66-" + e[0] + "-" + runID
		err := func() error {
			defer api("DELETE", "/v1/me/profiles/"+name)
			first, err := measure(pw, e[0], e[1], name)
			if err != nil {
				return err
			}
			if err := waitForVersion(name, 1); err != nil {
				return err
			}
			second, err := measure(pw, e[0], e[1], name)
			if err != nil {
				return err
			}
			profiles[e[0]] = []measurement{first, second}
			return waitForVersion(name, 2)
		}()
		if err != nil {
			return err
		}
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(map[string]any{"launches": all, "profile": profiles}); err != nil {
		return err
	}
	fmt.Print(buf.String())
	return nil
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
