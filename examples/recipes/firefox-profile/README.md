# Firefox that stays logged in between sessions

`profile` works on Firefox too: cookies (session ones included) and localStorage are restored at launch and saved when the session ends. Measured on prod with this launch: a cookie and a localStorage value set in the first session were back in the second. The generated code works in `browser.contexts()[0]`, the profile itself; a `newContext()` would not be saved. See [Persistent profiles](https://cdpfleet.com/docs/profiles).

- [Node.js](node/example.mjs)
- [Python](python/example.py)
- [Java](java/Example.java)
- [C#](csharp/Program.cs)
- [Go](go/main.go)

Set `CDPFLEET_API_KEY` and replace the placeholder proxy with yours. Install the client named at the top of each file.
