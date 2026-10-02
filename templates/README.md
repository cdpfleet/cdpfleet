# Starter projects

A ready-to-run project per language: the dependency file and a main program. Copy one, set your key and proxy, run.

| Language | Run |
|---|---|
| [Node.js](node) | `npm install && npm start` |
| [Python](python) | `pip install -r requirements.txt && python main.py` |
| [Java](java) | `mvn -q compile exec:java` |
| [C#](csharp) | `dotnet run` |
| [Go](go) | `go run .` (one-time driver setup in its README) |

Set `CDPFLEET_API_KEY` in your environment and replace the placeholder proxy in the program with yours. Each program launches Chromium through cdpfleet, opens a page and prints its title. Change the browser and options with the [code builder](https://cdpfleet.com/docs/builder).
