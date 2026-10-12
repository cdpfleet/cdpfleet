# Chrome that stays logged in between sessions

`profile: "shop-login"` restores the profile's cookies, localStorage and IndexedDB at launch and saves them when the session ends — log in once, reuse it on every later launch. The generated code works in `browser.contexts()[0]`, the profile itself; a `newContext()` would not be saved. See [Persistent profiles](https://cdpfleet.com/docs/profiles).

- [Node.js](node/example.mjs)
- [Python](python/example.py)
- [Java](java/Example.java)
- [C#](csharp/Program.cs)
- [Go](go/main.go)

Set `CDPFLEET_API_KEY` and replace the placeholder proxy with yours. Install the client named at the top of each file.
