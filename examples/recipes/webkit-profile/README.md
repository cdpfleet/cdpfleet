# WebKit that stays logged in between sessions

`profile` works on WebKit: cookies and localStorage set in one session came back in the next (measured on prod with this launch, on example.com). One WebKit rule to know: it refuses every cookie for a domain on the Public Suffix List, such as httpbin.org, so a profile can't keep a cookie there either; localStorage still works ([case 57](https://cdpfleet.com/docs/cases/inspect-and-edit-the-cookie-jar)). The generated code works in `browser.contexts()[0]`. See [Persistent profiles](https://cdpfleet.com/docs/profiles).

- [Node.js](node/example.mjs)
- [Python](python/example.py)
- [Java](java/Example.java)
- [C#](csharp/Program.cs)
- [Go](go/main.go)

Set `CDPFLEET_API_KEY` and replace the placeholder proxy with yours. Install the client named at the top of each file.
