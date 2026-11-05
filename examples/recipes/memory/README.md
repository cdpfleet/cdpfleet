# More memory for one heavy session

`"memory": 2` (or `3`, `4`) gives the session that many times your plan's memory, and it counts as that many times the threads. Measured on 2026-11-05 on a shared plan: the launch answered `"weight": 2, "memory_mb": 8192`; `5` or `"2x"` get `400 invalid_memory`. In [case 80](https://cdpfleet.com/docs/cases/tabs-and-memory-by-plan) a long Wikipedia article opened tab after tab ran out at 37 tabs with the default 4 GB (`session_memory_limit`) and reached the 50-tab cap with `"memory": 2`. Use it for big single-page apps, long scrolls and large PDFs or screenshots — not to go past 50 tabs. See the option [memory](https://cdpfleet.com/docs/options/memory).

- [Node.js](node/example.mjs)
- [Python](python/example.py)
- [Java](java/Example.java)
- [C#](csharp/Program.cs)
- [Go](go/main.go)

Set `CDPFLEET_API_KEY` and replace the placeholder proxy with yours. Install the client named at the top of each file.
