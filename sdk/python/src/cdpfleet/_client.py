"""Synchronous cdpfleet client. Spec shared by every language: sdk/SPEC.md."""
from __future__ import annotations

import http.client
import json
import os
import random
import re
import time
import urllib.error
import urllib.parse
import urllib.request
from typing import Any, Callable, Dict, List, Optional, Tuple

VERSION = "0.1.0"
USER_AGENT = f"cdpfleet-sdk-python/{VERSION}"
QUOTA = {"daily_quota_exhausted", "hourly_quota_exhausted"}
DEFAULT_STARTER_URL = "https://starter.cdpfleet.com"
DEFAULT_API_URL = "https://cdpfleet.com"


class CdpfleetError(Exception):
    """A non-success response from cdpfleet: `status` (0 for network), `code` (body `error`), `body`."""

    def __init__(self, status: int, code: Optional[str] = None, body: Any = None, message: Optional[str] = None):
        self.status = status
        self.code = code
        self.body = body
        self.message = message or f"cdpfleet: {status} {code or ''}".strip()
        super().__init__(self.message)


def _parse(text: str) -> Any:
    try:
        return json.loads(text) if text else None
    except ValueError:
        return {"error": text[:200]}


def _code(body: Any) -> Optional[str]:
    return body.get("error") if isinstance(body, dict) else None


def _http(method: str, url: str, headers: Dict[str, str], body: Any, timeout: float) -> Tuple[int, Any, Any]:
    """Returns (status, parsed body, response headers); raises only on network errors."""
    data = None if body is None else json.dumps(body).encode()
    req = urllib.request.Request(url, data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as res:
            return res.status, _parse(res.read().decode("utf-8", "replace")), res.headers
    except urllib.error.HTTPError as err:
        text = err.read().decode("utf-8", "replace") if err.fp else ""
        return err.code, _parse(text), err.headers


def _sleep_retry(attempt: int, retry_after: Optional[str]) -> float:
    try:
        after = float(retry_after) if retry_after else 0.0
    except ValueError:
        after = 0.0
    return (after or min(2 ** (attempt - 1), 10)) + random.random() * 0.5


class Cdpfleet:
    """Launches remote browsers on cdpfleet and calls the account API."""

    def __init__(
        self,
        api_key: Optional[str] = None,
        starter_url: str = DEFAULT_STARTER_URL,
        api_url: str = DEFAULT_API_URL,
        max_wait: float = 120,
    ):
        api_key = api_key or os.environ.get("CDPFLEET_API_KEY")
        if not api_key:
            raise ValueError("cdpfleet: no API key (pass api_key or set CDPFLEET_API_KEY)")
        self.api_key = api_key
        self.starter_url = starter_url.rstrip("/")
        self.api_url = api_url.rstrip("/")
        self.max_wait = max_wait

    def _headers(self, json_body: bool = False) -> Dict[str, str]:
        h = {"x-api-key": self.api_key, "user-agent": USER_AGENT}
        if json_body:
            h["content-type"] = "application/json"
        return h

    def _launch_raw(self, browser: str, options: Dict[str, Any]) -> Any:
        # Retries fleet backpressure (503) and rate limits (429) as the API asks.
        deadline = time.monotonic() + self.max_wait
        attempt = 0
        while True:
            attempt += 1
            try:
                status, body, headers = _http(
                    "POST", f"{self.starter_url}/{browser}/session", self._headers(True), options, 60
                )
            except (OSError, http.client.HTTPException) as err:
                if time.monotonic() > deadline:
                    raise CdpfleetError(0, "network", {"error": "network"}, f"cdpfleet: launch failed: {err}") from err
                time.sleep(min(2 ** attempt, 10))
                continue
            if status == 200:
                return body
            code = _code(body)
            retryable = status == 503 or (status == 429 and code not in QUOTA)
            if not retryable or time.monotonic() > deadline:
                raise CdpfleetError(status, code, body)
            time.sleep(_sleep_retry(attempt, headers.get("retry-after") if headers else None))

    def launch(self, browser: str, options: Optional[Dict[str, Any]] = None, **kwargs: Any) -> "Session":
        """Launches `browser` (chrome, chromium, firefox, camoufox, ...); options are the JSON body."""
        opts = {**(options or {}), **kwargs}
        return Session(self, self._launch_raw(browser, opts), opts)

    def _request(self, method: str, path: str, body: Any = None) -> Any:
        status, data, _ = _http(method, f"{self.api_url}{path}", self._headers(body is not None), body, 30)
        if not 200 <= status < 300:
            raise CdpfleetError(status, _code(data), data)
        return data

    def me(self) -> Any:
        return self._request("GET", "/v1/me")

    def usage(self) -> Any:
        return self._request("GET", "/v1/me/usage")

    def threads(self) -> Any:
        return self._request("GET", "/v1/me/threads")

    def sessions(self, days: Optional[int] = None, limit: Optional[int] = None) -> Any:
        q = urllib.parse.urlencode({k: v for k, v in (("days", days), ("limit", limit)) if v is not None})
        return self._request("GET", f"/v1/me/sessions{'?' + q if q else ''}")

    def recordings(self) -> Any:
        return self._request("GET", "/v1/me/recordings")

    def recording_url(self, id: str) -> str:
        return self._request("GET", f"/v1/me/recordings/{id}/url")["url"]

    def delete_recording(self, id: str) -> Any:
        return self._request("DELETE", f"/v1/me/recordings/{id}")

    def proxy_balance(self) -> Any:
        return self._request("GET", "/v1/me/proxy-balance")

    def end_session(self, session_id: str) -> None:
        """Ends a session at once (404 = already ended, not an error)."""
        try:
            self._request("DELETE", f"/v1/me/threads/{session_id}")
        except CdpfleetError as err:
            if err.status != 404:
                raise


def _with_key(options: Dict[str, Any], api_key: str) -> Dict[str, Any]:
    return {**options, "headers": {**(options.get("headers") or {}), "x-api-key": api_key}}


def _already_connected(err: BaseException) -> bool:
    return re.search("session_already_connected", str(err)) is not None


class _SessionBase:
    def __init__(self, fleet: Cdpfleet, raw: Any, options: Dict[str, Any]):
        self.fleet = fleet
        self.raw = raw
        self.id: str = raw.get("sessionId")
        self.ws_url: str = raw.get("wsUrl")
        self.cdp_url: Optional[str] = raw.get("cdpUrl")
        self.browser_name: Optional[str] = raw.get("browser")
        self.resource_class: Optional[str] = raw.get("resource_class")
        self.weight: Any = raw.get("weight")
        self.expires_at: Any = raw.get("expires_at")
        self.keep_alive: bool = options.get("keep_alive") is True
        self.browsers: List[Any] = []
        self.closed = False

    def _router(self, path: str, body: Dict[str, Any]) -> Any:
        host = urllib.parse.urlsplit(self.ws_url).netloc
        status, data, _ = _http("POST", f"https://{host}{path}", self.fleet._headers(True), body, 30)
        if not 200 <= status < 300:
            raise CdpfleetError(status, _code(data), data)
        return data

    def _live_link(self, mode: str) -> Any:
        return self._router("/admin/session/live", {"session_id": self.id, "mode": mode})

    def _swap_proxy(self, proxy: Any, proxy_rules: Any) -> Any:
        body: Dict[str, Any] = {"session_id": self.id, "proxy": proxy}
        if proxy_rules:
            body["proxy_rules"] = proxy_rules
        return self._router("/admin/session/proxy", body)

    def _cdp_url_or_raise(self) -> str:
        if not self.cdp_url:
            raise ValueError("cdpfleet: this session has no cdp_url (launch it with cdp=True)")
        return self.cdp_url

    def __repr__(self) -> str:
        return f"<{type(self).__name__} id={self.id!r} browser={self.browser_name!r}>"


class Session(_SessionBase):
    """A launched browser. Connect your own Playwright (sync API) to it; `with` closes it."""

    # Right after a disconnect the router may still count the old connection (409
    # session_already_connected): a keep_alive re-attach retries that for up to 10 s.
    def _attach(self, open_: Callable[[], Any]) -> Any:
        i = 0
        while True:
            try:
                browser = open_()
                self.browsers.append(browser)
                return browser
            except Exception as err:
                if i >= 10 or not _already_connected(err):
                    raise
                i += 1
                time.sleep(1)

    def connect(self, browser_type: Any, **options: Any) -> Any:
        """Connects a Playwright browser type (e.g. `p.chromium`) over the Playwright protocol."""
        return self._attach(lambda: browser_type.connect(self.ws_url, **_with_key(options, self.fleet.api_key)))

    def connect_over_cdp(self, browser_type: Any, **options: Any) -> Any:
        """Connects over CDP (launch with `cdp=True`): Playwright's connect_over_cdp."""
        url = self._cdp_url_or_raise()
        return self._attach(lambda: browser_type.connect_over_cdp(url, **_with_key(options, self.fleet.api_key)))

    def live_link(self, mode: str = "view") -> Any:
        """A single-use live-view link (`view` or `control`); works before your code connects."""
        return self._live_link(mode)

    def swap_proxy(self, proxy: Any, proxy_rules: Any = None) -> Any:
        """Swaps the proxy of a session launched with proxy_updatable; pass the rules again."""
        return self._swap_proxy(proxy, proxy_rules)

    def end(self) -> None:
        """Ends the session now (keep_alive sessions otherwise live until inactivity_timeout)."""
        self.fleet.end_session(self.id)

    def close(self) -> None:
        """Closes what this session connected; ends keep_alive sessions too. Safe to call twice."""
        if self.closed:
            return
        self.closed = True
        for b in self.browsers:
            try:
                b.close()
            except Exception:
                pass
        if self.keep_alive:
            self.end()

    def __enter__(self) -> "Session":
        return self

    def __exit__(self, *exc: Any) -> None:
        self.close()
