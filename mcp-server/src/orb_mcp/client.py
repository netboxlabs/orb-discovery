from __future__ import annotations

import httpx


class AgentClient:
    """Async HTTP client for orb-discovery agent REST APIs."""

    def __init__(self, base_url: str, timeout: float = 30.0) -> None:
        self._client = httpx.AsyncClient(base_url=base_url, timeout=timeout)

    async def __aenter__(self) -> "AgentClient":
        return self

    async def __aexit__(self, *args: object) -> None:
        await self._client.aclose()

    async def post_policy(self, yaml_body: str) -> httpx.Response:
        return await self._client.post(
            "/api/v1/policies",
            content=yaml_body.encode(),
            headers={"Content-Type": "application/x-yaml"},
        )

    async def delete_policy(self, name: str) -> httpx.Response:
        return await self._client.delete(f"/api/v1/policies/{name}")

    async def get_status(self) -> httpx.Response:
        return await self._client.get("/api/v1/status")

    async def get_policies(self) -> httpx.Response:
        return await self._client.get("/api/v1/policies")

    async def get_capabilities(self) -> httpx.Response:
        return await self._client.get("/api/v1/capabilities")
