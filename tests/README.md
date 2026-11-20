# e2e tests
Run via `scripts/e2e.sh <config>`. Helpers live in `harness_test.go` (package `e2e`).
- `GatewayURL()`, `Config()`, `MockURL(port)`; env `GATEWAY_URL`, `CONFIG`, `MOCK_HOST`.
- `RequireConfig(t, "gateway.yaml", ...)`: skips without `GATEWAY_URL` or on other configs.
- `NewClient(t)`: client bound to its own `127.0.0.N` (N from 2). `RequireDistinctIPs(t)` skips where unbindable (macOS).
- `c.Req(method, path, body, "Name", "value", ...)`, `c.Try`, `c.Get` return `*Resp` (`Status`, `Header`, `Body`, `Start`, `Elapsed`, `JSON`, `Map`).
- `ResetMocks(t, ports...)` (all of 3001 to 3006 by default), `SetMockHealth`, `SetMockDefault`.
- `MockRequests`, `LastMockRequest`, `Gaps`; types `MockRequest`, `Echo`.
- `WindowBoundary`, `WaitWindowStart`, `WaitWindowRoom` for clock-aligned windows.
- `RestoreMockHealth`, `WaitBreakerCooldown` restore gateway state before a test returns.
- `Eventually`, `Timed`, `Within`, `RequireError` (JSON error contract).
