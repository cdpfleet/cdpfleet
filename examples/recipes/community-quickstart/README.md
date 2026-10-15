# Community plan: the included proxy, US and sticky

On the free [Community plan](https://cdpfleet.com/docs/pricing#community) the proxy is included: leave `proxy` out, or send a `cdpfleet-plan` token like this one for a US exit that stays the same for 30 minutes — also for later sessions that reuse the session name. The dashboard's Proxies → Community proxy tab builds these tokens. The included proxy is shared by all Community users, so it's slower at peak times. Headless here (1 of the plan's 2 threads; headful takes both). See [a first free scrape](https://cdpfleet.com/docs/cases/community-first-scrape).

- [Node.js](node/example.mjs)
- [Python](python/example.py)
- [Java](java/Example.java)
- [C#](csharp/Program.cs)
- [Go](go/main.go)

Set `CDPFLEET_API_KEY` and replace the placeholder proxy with yours. Install the client named at the top of each file.
