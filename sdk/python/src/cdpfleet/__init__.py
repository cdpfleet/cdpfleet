"""cdpfleet: launch real remote browsers and drive them with your own Playwright."""
from ._async import AsyncCdpfleet, AsyncSession
from ._client import VERSION, Cdpfleet, CdpfleetError, Session

__version__ = VERSION
__all__ = ["Cdpfleet", "AsyncCdpfleet", "Session", "AsyncSession", "CdpfleetError", "__version__"]
