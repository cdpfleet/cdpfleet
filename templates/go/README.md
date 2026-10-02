# Go starter

playwright-go v0.6000.0 can't download the Playwright 1.60 driver itself, so build it once from npm (needs Node.js 18+):

```bash
mkdir -p ~/.cache/pw-driver-1.60.0 && cd ~/.cache/pw-driver-1.60.0 &&
  curl -sL https://registry.npmjs.org/playwright-core/-/playwright-core-1.60.0.tgz | tar xz &&
  ln -sf "$(command -v node)" node
export PLAYWRIGHT_DRIVER_PATH=~/.cache/pw-driver-1.60.0
```

Then `CDPFLEET_API_KEY=… go run .`
