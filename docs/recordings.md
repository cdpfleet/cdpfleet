# Session recordings

Every session can be saved as a video you download later — separate from [live view](live-view.md), which streams frames to your running code. Recordings are made on the server and kept for you to review afterwards.

- **Headful** sessions (the default — a virtual display) are recorded from that display automatically.
- **Headless** sessions are recorded only with `"cdp": true` or `"live_view": true`, which give the recorder a way to capture the page. Other headless sessions are never recorded.
- Opt out of recording for a session with `"record": false`.

```
POST https://starter.cdpfleet.com/chrome/session
{ "proxy": "http://user:pass@proxy.example.com:8080", "record": false }
```

Headful tip: Playwright sizes each context's window to its viewport (1280×720 by default), so the recording shows that window on a larger desktop. Create contexts with `viewport: null` (Python `no_viewport=True`) and the page fills the whole display (`screen_size`, 1920×1080 by default). Headless recordings capture the page itself and always fill the frame.

Videos are **480p, 1 fps MP4**, about **1.2 MB per minute** — sized for reviewing what happened. Recording plans can choose **720p** (1.5× the price) or **1080p** (2.5×).

## Free tier and plans

- Free on every account: the **first 15 minutes of each session**, and the **most recent 180 minutes across your account**, kept for **7 days**.
- A recording plan raises all three limits — up to **4 hours per session**, up to **100,000 minutes per account**, kept up to **30 days** — from **$10/month**. Set the limits and the video quality, and see the price, in the [dashboard](https://cdpfleet.com/app) under **Sessions → Session Recording Settings**.
- Over the limits, the oldest recordings are deleted first.

Find, play and download your recordings in the dashboard under **Sessions**.

Full page: [cdpfleet.com/docs/recordings](https://cdpfleet.com/docs/recordings).
