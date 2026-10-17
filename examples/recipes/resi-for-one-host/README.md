# Residential IPs for one strict host, your own proxy for the rest

Keep your own proxy for everything, and send only the host that blocks it through the cdpfleet residential proxy, from the country you need. You then pay residential prices for that host's traffic alone. Measured with ip-api.com as the rule's host: every lookup through `cdpfleet-resi-country-de` came from a German home ISP (Deutsche Telekom, Vodafone, 1&1, EWE), and through `-country-us` from US ones (Comcast, Charter, Cablevision). The same page's other hosts went out through our own proxy. Add `-tier-2` to `-tier-4` to the token for cheaper tiers ([case 51](https://cdpfleet.com/docs/cases/resi-tiers-mix) mixes tiers like this). The rule's `name` labels its traffic on your dashboard's Proxy traffic page.

- [Node.js](node/example.mjs)
- [Python](python/example.py)
- [Java](java/Example.java)
- [C#](csharp/Program.cs)
- [Go](go/main.go)

Set `CDPFLEET_API_KEY` and replace the placeholder proxy with yours. Install the client named at the top of each file.
