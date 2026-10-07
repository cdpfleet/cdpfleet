# Session recordings

Every session can be saved as a video you download later — separate from [live view](live-view.md), which streams frames to your running code. Recordings are made on the server and kept for you to review afterwards.

- **Headful** sessions (the default — a virtual display) are recorded from that display automatically.
- **Headless** sessions are recorded only with `"cdp": true`, which gives the recorder a way to capture the page.
- Opt out of recording for a session with `"record": false`.

```
POST https://starter.cdpfleet.com/chrome/session
{ "proxy": "http://user:pass@proxy.example.com:8080", "record": false }
```

Videos are **480p, 1 fps MP4**, about **1.2 MB per minute** — sized for reviewing what happened, not for pixel-perfect playback.

## Free tier and plans

- Free on every account: the **first 15 minutes of each session**, and the **most recent 180 minutes across your account**, kept for **7 days**.
- A recording plan raises all three limits — up to **4 hours per session**, up to **100,000 minutes per account**, kept up to **30 days** — from **$10/month**. Set the limits and see the price in the [dashboard](https://cdpfleet.com/app) under **History**.
- Over the limits, the oldest recordings are deleted first.

Find, play and download your recordings in the dashboard under **History**.

Full page: [cdpfleet.com/docs/recordings](https://cdpfleet.com/docs/recordings).
