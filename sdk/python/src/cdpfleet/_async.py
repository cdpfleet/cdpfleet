"""asyncio cdpfleet client: HTTP runs in a worker thread, Playwright calls use its async API."""
from __future__ import annotations

import asyncio
from typing import Any, Awaitable, Callable, Dict, Generator, Optional

from ._client import (
    DEFAULT_API_URL,
    DEFAULT_STARTER_URL,
    Cdpfleet,
    _already_connected,
    _SessionBase,
    _with_key,
)


class AsyncCdpfleet:
    """The async twin of `Cdpfleet`: every method is a coroutine."""

    def __init__(
        self,
        api_key: Optional[str] = None,
        starter_url: str = DEFAULT_STARTER_URL,
        api_url: str = DEFAULT_API_URL,
        max_wait: float = 120,
    ):
        self._sync = Cdpfleet(api_key=api_key, starter_url=starter_url, api_url=api_url, max_wait=max_wait)

    @property
    def api_key(self) -> str:
        return self._sync.api_key

    def launch(self, browser: str, options: Optional[Dict[str, Any]] = None, **kwargs: Any) -> "_Launch":
        """`session = await fleet.launch(...)` or `async with fleet.launch(...) as session:`."""
        opts = {**(options or {}), **kwargs}
        return _Launch(self._launch(browser, opts))

    async def _launch(self, browser: str, opts: Dict[str, Any]) -> "AsyncSession":
        raw = await asyncio.to_thread(self._sync._launch_raw, browser, opts)
        return AsyncSession(self, raw, opts)

    async def me(self) -> Any:
        return await asyncio.to_thread(self._sync.me)

    async def usage(self) -> Any:
        return await asyncio.to_thread(self._sync.usage)

    async def threads(self) -> Any:
        return await asyncio.to_thread(self._sync.threads)

    async def sessions(self, days: Optional[int] = None, limit: Optional[int] = None) -> Any:
        return await asyncio.to_thread(self._sync.sessions, days, limit)

    async def recordings(self) -> Any:
        return await asyncio.to_thread(self._sync.recordings)

    async def recording_url(self, id: str) -> str:
        return await asyncio.to_thread(self._sync.recording_url, id)

    async def delete_recording(self, id: str) -> Any:
        return await asyncio.to_thread(self._sync.delete_recording, id)

    async def proxy_balance(self) -> Any:
        return await asyncio.to_thread(self._sync.proxy_balance)

    async def end_session(self, session_id: str) -> None:
        await asyncio.to_thread(self._sync.end_session, session_id)


class _Launch:
    """Awaitable that is also an async context manager, so both launch styles work."""

    def __init__(self, coro: Awaitable["AsyncSession"]):
        self._coro = coro
        self._session: Optional[AsyncSession] = None

    def __await__(self) -> Generator[Any, None, "AsyncSession"]:
        return self._coro.__await__()

    async def __aenter__(self) -> "AsyncSession":
        self._session = await self._coro
        return self._session

    async def __aexit__(self, *exc: Any) -> None:
        if self._session is not None:
            await self._session.close()


class AsyncSession(_SessionBase):
    """A launched browser for Playwright's async API; `async with` closes it."""

    def __init__(self, fleet: AsyncCdpfleet, raw: Any, options: Dict[str, Any]):
        super().__init__(fleet._sync, raw, options)
        self.async_fleet = fleet

    # Right after a disconnect the router may still count the old connection (409
    # session_already_connected): a keep_alive re-attach retries that for up to 10 s.
    async def _attach(self, open_: Callable[[], Awaitable[Any]]) -> Any:
        i = 0
        while True:
            try:
                browser = await open_()
                self.browsers.append(browser)
                return browser
            except Exception as err:
                if i >= 10 or not _already_connected(err):
                    raise
                i += 1
                await asyncio.sleep(1)

    async def connect(self, browser_type: Any, **options: Any) -> Any:
        """Connects a Playwright async browser type (e.g. `p.chromium`) over the Playwright protocol."""
        return await self._attach(lambda: browser_type.connect(self.ws_url, **_with_key(options, self.fleet.api_key)))

    async def connect_over_cdp(self, browser_type: Any, **options: Any) -> Any:
        """Connects over CDP (launch with `cdp=True`): Playwright's connect_over_cdp."""
        url = self._cdp_url_or_raise()
        return await self._attach(lambda: browser_type.connect_over_cdp(url, **_with_key(options, self.fleet.api_key)))

    async def live_link(self, mode: str = "view") -> Any:
        """A single-use live-view link (`view` or `control`); works before your code connects."""
        return await asyncio.to_thread(self._live_link, mode)

    async def swap_proxy(self, proxy: Any, proxy_rules: Any = None) -> Any:
        """Swaps the proxy of a session launched with proxy_updatable; pass the rules again."""
        return await asyncio.to_thread(self._swap_proxy, proxy, proxy_rules)

    async def end(self) -> None:
        """Ends the session now (keep_alive sessions otherwise live until inactivity_timeout)."""
        await asyncio.to_thread(self.fleet.end_session, self.id)

    async def close(self) -> None:
        """Closes what this session connected; ends keep_alive sessions too. Safe to call twice."""
        if self.closed:
            return
        self.closed = True
        for b in self.browsers:
            try:
                await b.close()
            except Exception:
                pass
        if self.keep_alive:
            await self.end()

    async def __aenter__(self) -> "AsyncSession":
        return self

    async def __aexit__(self, *exc: Any) -> None:
        await self.close()
