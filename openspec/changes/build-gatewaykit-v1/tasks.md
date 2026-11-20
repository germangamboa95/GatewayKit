# Tasks

## Parallel execution

Nine agents plus the orchestrator. Each agent works in its own worktree and branch and codes only against the Wave 0 contracts. The orchestrator merges in milestone order (M0, M1, then M2 to M4, then M5) and runs the design D13 gates before each merge. It writes each gate report to `openspec/changes/build-gatewaykit-v1/gates/M<n>.md` without pausing. Agents run e2e only through `scripts/e2e.sh` or `scripts/ci.sh`, which hold the Compose lock (design D13).

| Agent | Wave 0 | Wave 1 | Wave 2 and 3 |
| --- | --- | --- | --- |
| orchestrator | 1, 2.4 | 11.1 | 11.5, 12.4, 13.5, 14.4, 15.2, 15.3 |
| `harness` | 2.1 to 2.3 | 7.3 | 11.4, 12.3, 13.4, 15.1 |
| `yaml` | | 3 | 14.3 |
| `config` | | 4 | 11.3 |
| `http` | | 5 | 11.2 |
| `proxy` | | 6 | 13.1 |
| `mock` | | 7.1, 7.2 | 13.3 |
| `policy` | | 8 | 12.1, 12.2 |
| `resilience` | | 9 | 13.2 |
| `transform` | | 10 | 14.1, 14.2 |

When each piece of work starts:

- **Library agents** (`yaml`, `config`, `http`, `proxy`, `policy`, `resilience`, `transform`) start when 1.6 merges the contracts, fixtures and configs.
- **`mock`** starts when 2.4 merges the build skeleton.
- **The `harness` agent's 7.3** starts when 7.1 merges.
- **An F package** starts when its agent's Wave 1 group merges. It needs only the contracts, the fixtures and its own libraries, because `ErrorResponse` and the pass-through constructors ship in Wave 0.
- **An e2e scenario file** is written once 7.3 merges and must pass once its F package and M1 have merged.
- **Groups 12, 13 and 14** touch separate packages. They build in parallel and merge one after another.

## 1. Wave 0a: contracts and configs (orchestrator)

- [x] 1.1 Create the integration branch `feat/gatewaykit-v1` in a worktree per the apply guidance; verify `git worktree list` shows it
- [x] 1.2 W0.2: write `internal/contract` with the base_spec.md Wave 0 block and the seven fixes in design D2; implement `ErrorResponse` and `Classify`, stub the rest; verify `go vet ./...` and unit tests for every config spec §12 error body and each `Classify` case in design D7
- [x] 1.3 W0.2: write `internal/yamlparse` (`Node`, `Kind`, stub `ParseYAML`) and pass-through `New` stubs for `internal/middleware/ratelimit`, `.../auth`, `.../reqtransform`, `.../resptransform`, `internal/upstream` and `internal/healthcheck`; verify `go build ./...`
- [x] 1.4 W0.2: write `internal/fixtures` with one normalized `Route` per sample route; verify a unit test checks each fixture against design D10 (orders timeout 5s and retry 3/exponential/1s, products weights 3:1 with health check, legacy transforms, internal auth and breaker, inherited global limits, `MethodList` order)
- [x] 1.5 Write `configs/gateway.yaml` (design D10) and `configs/variant-a.yaml`, `variant-b.yaml` and `variant-c.yaml` (design D11); verify `gateway.yaml` and `variant-c.yaml` match their design blocks byte for byte, and A and B match config spec §13 except for the `400N` to `300(N+1)` port remap
- [x] 1.6 Merge group 1 and dispatch the Wave 1 library agents; verify `go build ./...` and `go vet ./...` pass on the integration branch

## 2. Wave 0b: build skeleton (agent `harness`, parallel with group 1)

- [x] 2.1 W0.1: write a root `Dockerfile` (`golang:1.27.4`, `CGO_ENABLED=0`, static binary into `scratch`) and turn `main.go` into a stub that listens on 8080; add `mock-upstream/` and `tests/` modules with zero-require `go.mod` files, stub mains (the mock listens on `$PORT`) and Dockerfiles pinned to `golang:1.27.4`; verify `go build ./...` in all three modules and `docker build` of all three images
- [x] 2.2 W0.1: write `docker-compose.yml` per base_spec.md "Docker Compose", with `profiles: ["test"]`, `CONFIG`, `GATEWAY_URL=http://localhost:${GATEWAY_PORT:-8080}`, and ports 8080, 9090 and 3001 to 3006; verify `docker compose up -d` lists 7 running containers and `curl localhost:8080/` answers
- [x] 2.3 W0.1: write `scripts/e2e.sh` and `scripts/ci.sh` per design D11 and D13; verify:
  - `scripts/ci.sh` exits 0 with the stub tests.
  - A temp copy of the repo with an added `require` line fails the standard-library check, naming the module (run with `GOPROXY=off`).
  - A second `scripts/e2e.sh` started while one runs exits on the lock.
- [x] 2.4 [orchestrator] Merge group 2 and write `gates/M0.md` (contract files and the D2 fixes, fixtures, standard-library check output, `docker compose ps`); verify `scripts/ci.sh` passes on the integration branch

## 3. YAML subset parser (A0, agent `yaml`)

- [x] 3.1 Build the line scanner: indentation width, tab-in-indent error, comments outside quotes, leading `---` and trailing `...`; verify a unit test per rule
- [x] 3.2 Build block mappings and sequences on an indent stack (`-` at parent indent or deeper, mapping items, empty value as null, duplicate key error, keys with dots and dashes); verify table tests compare node trees, including `Line` and `Col`
- [x] 3.3 Parse plain, double-quoted (`\"`, `\\`, `\n`, `\t`, `\uXXXX`) and single-quoted (`''`) scalars and set `Quoted`; verify escape table tests
- [x] 3.4 Parse single-line flow sequences and mappings, nested; verify tests for `[a, "b", 3]`, `{ k: v }`, nesting, and an error when a flow value spans lines
- [x] 3.5 Reject each unsupported construct (anchors, aliases, `<<`, tags, `|`, `>`, multiple documents, `?`, multi-line plain values) with its line number; verify one test per construct asserts the line
- [x] 3.6 Parse all four files in `configs/`; verify golden node-tree tests pass

## 4. Config loader (A1 to A4, agent `config`)

- [x] 4.1 A1: parse durations (`^\d+(\.\d+)?(ms|s|m|h)$`, bare integer as seconds, greater than zero unless the field allows `0s`); verify table tests for `"500ms"`, `"1.5m"`, `30`, `"5 s"` rejected, and `"0s"` accepted only for `initial_delay`
- [x] 4.2 A2: resolve the config path (CLI argument, `--config`, `GATEWAY_CONFIG`, `./gateway.yaml`) and report every location tried when none exists; verify tests with temp dirs and env vars
- [x] 4.3 A2: decode node trees per design D4 (typing rules, unknown-key warnings with location, lenient placement and both-places errors, route `rate_limit: false` per design D5); verify tests on hand-built node trees
- [x] 4.4 A3: validate every field and cross-field rule in the config-loading spec, collecting all errors as `file:line:col: field.path: message`; verify one test per rule plus the config-loading spec's multi-error scenario
- [x] 4.5 A3: emit the four config spec §11 warnings; verify a test captures each one
- [x] 4.6 A4: normalize per config spec §10 (precedence chain, rate-limit merge, targets from `url`, `MethodList`, lowercase header names, compiled variables, mappings and envelopes with scope and conflict errors); verify the sample's hand-built tree normalizes to `internal/fixtures`, and config A gets every default
- [x] 4.7 Wire `Load(path)` end to end once group 3 merges; verify the sample loads with zero errors and zero warnings, all four `configs/` files load, and `balance: random` fails with its location

## 5. HTTP core (B1 to B3, agent `http`)

- [x] 5.1 B1: build the root handler per design D6 and answer exactly `GET /health` with floored `uptime_seconds`; verify `httptest` tests, including `HEAD /health` and `POST /health` being routed
- [x] 5.2 B2: route by longest prefix on segment boundaries, `/` as catch-all, 404, and 405 with `Allow` built from `MethodList`; verify a table test per request-routing scenario
- [x] 5.3 B3: build the pipeline runner and the access log line; verify tests that an early exit skips later stages and that the log line carries method, path, status, route and duration

## 6. Proxy core (C1 to C3, agent `proxy`)

- [x] 6.1 C1: rewrite paths (strip_prefix, empty result as `/`, base path join, query kept); verify the four config spec §4 table rows plus query and double-slash cases
- [x] 6.2 C2: build the upstream client per design D7 (shared transport, buffered body, hop-by-hop removal, `Host`, `X-Forwarded-For`, `-Proto`, `-Host`, no redirects, `Accept-Encoding` pass-through or `identity`); verify `httptest` tests assert the request the upstream received and that upstream status, headers and body come back unchanged
- [x] 6.3 C3: apply the per-attempt timeout through the full body; verify tests with a slow-body fake (504), a closed-socket fake (502) and a canceled client context (aborted, not classified)
- [x] 6.4 Build the single-attempt terminal handler (first target, one attempt, `FromUpstream` set); verify an `httptest` round trip

## 7. Mock and e2e harness (D1 to D3, agents `mock` and `harness`)

- [x] 7.1 [mock] D1: build the mock per the mock-upstream spec; verify unit tests in `mock-upstream/`, including `HEALTH_PATH` staying out of the log and ignoring the default override
- [x] 7.2 [mock] D2: point Compose at the real mock image; verify `curl localhost:3005/__mock/requests` returns `[]` and a proxied response from 3005 carries `Server: mock`
- [x] 7.3 [harness] D3: build the e2e harness in `tests/` per design D11 (`GATEWAY_URL`, `CONFIG`, per-config scenario selection, mock reset, a client per test with its own `127.0.0.N` source IP, window-boundary helper, state-restore helpers, request-log and timing helpers); verify `go vet ./...` and a smoke test against `PORT=3001 go -C mock-upstream run .`

## 8. Rate limiter and auth (E1, E8, agent `policy`)

- [x] 8.1 E1: build clock-aligned fixed windows, bounded sliding logs, per-route stores keyed by IP or global, the reset and remaining values from the rate-limiting spec, and the 2x-window idle sweep; verify fake-clock unit tests per rate-limiting spec scenario
- [x] 8.2 E8: build the API-key authenticator (constant time, case-insensitive header, missing vs invalid); verify unit tests

## 9. Balancer, breaker, retry (E2 to E4, agent `resilience`)

- [x] 9.1 E2: build round robin and smooth weighted round robin over the healthy subset, plus target pool state; verify:
  - 3:1 yields A, A, B, A, and exactly 3:1 in any 4 consecutive picks.
  - Unhealthy targets are skipped.
  - The pick reports false when no target is healthy.
  - Two pools keep separate rotation.
- [x] 9.2 E3: build the breaker state machine with `Release`; verify a fake-clock test per row of the config spec §7 state table, a released trial admitting the next request, and `retry_after` rounding up to at least 1
- [x] 9.3 E4: build the retry policy and backoff; verify fixed and exponential delays and `on` filtering for the 502 and 504 stand-ins

## 10. Transforms (E5 to E7, agent `transform`)

- [x] 10.1 E5: resolve variables and transform headers (remove then add, managed headers untouched); verify unit tests per traffic-transforms variable and header scenario
- [x] 10.2 E6: build dot-path get and set and the body mapper per design D8 (`UseNumber`, trailing data rejected, ordered output, array indexes, missing keys omitted, `null` copied); verify the config spec §9 in/out example byte for byte
- [x] 10.3 E7: build the envelope (`$body` as raw JSON, raw string or `null`, ordered output); verify the sample `/api/legacy` envelope and variant B's exact bytes `{"result":{"payload":[1,2],"ok":true},"route":"/v2/admin"}`

## 11. M1 core: wire and boot (F1)

- [x] 11.1 [orchestrator] Merge groups 3 to 7 as each passes; verify the design D13 unit gates pass on the integration branch after each merge
- [x] 11.2 [http] F1: write `main()` to load, normalize, wire each route's chain from the design D2 constructors with the 6.4 terminal handler, serve, and log each route's middleware; verify `httptest` tests on the assembled handler for the sample routes, and that `routes: []` serves `/health` with 200 and everything else with 404
- [x] 11.3 [config] Write the root-module exit-code test per design D11; verify `go test .`:
  - `testdata/broken.yaml` (three errors) exits 1 and prints each error with `broken.yaml:<line>:<col>`.
  - A missing config exits 1 and lists all four locations.
- [x] 11.4 [harness] Write e2e `tests/core_test.go`; verify `scripts/e2e.sh` passes on all four configs. It covers:
  - `/health`, and `HEAD /health` being routed.
  - Proxy pass-through of status, headers and body.
  - strip_prefix and base path.
  - 404, 405 with `Allow`, longest prefix and the segment boundary.
  - Variant A default proxying.
  - Variant B: port 9090, and the route timeout beating the global one.
  - Variant C: 504 on `/c/unlimited` with a 2500ms mock delay.
- [x] 11.5 [orchestrator] Merge 11.2 to 11.4 and write `gates/M1.md` (e2e report, curl transcript of health, proxy, 404 and 405, broken-config output); verify `scripts/ci.sh` passes

## 12. M2 global features (F2, F3)

- [x] 12.1 [policy] F2: fill `internal/middleware/ratelimit` (client IP from the socket address, 429 body and headers, `X-RateLimit-*` on allowed responses, none when disabled); verify unit tests, including `X-Forwarded-For` being ignored
- [x] 12.2 [policy] F3: fill `internal/middleware/auth`; verify unit tests for both 401 bodies, the case-insensitive header, and the key forwarded unchanged
- [x] 12.3 [harness] Write e2e `tests/ratelimit_test.go` and `tests/auth_test.go`; verify `scripts/e2e.sh` passes on the sample, variant B and variant C once 12.1 and 12.2 merge. They cover:
  - **Sample limits:** `/api/users` 30 per 60s sliding; `/api/orders` 10 per 10s fixed, using the window helper; 100 per 60s inherited.
  - **Counting:** separate buckets per route; 404, 405 and `/health` not counted; a spoofed `X-Forwarded-For` ignored.
  - **Variant B:** the global bucket shared by two source IPs.
  - **Variant C:** `/c/limited` returns 429 on the sixth request; `/c/unlimited` carries no limit headers.
  - **Auth:** missing, wrong and valid keys; failed auth uses quota; variant B's `Authorization` header.
- [x] 12.4 [orchestrator] Merge groups 8 and 12 and write `gates/M2.md` with 429 and 401 transcripts; verify `scripts/ci.sh` passes

## 13. M3 resilience (F5, F7)

- [x] 13.1 [proxy] F5: fill `internal/upstream` per design D6 and swap it in for the terminal handler in `main.go`; verify unit tests with a fake clock and client:
  - Retry waits; a re-pick on each attempt; the last response returned.
  - The breaker checked before each attempt.
  - Half-open with all targets unhealthy releases the trial.
  - A client cancel is not recorded.
  - `FromUpstream` is false on gateway errors.
- [x] 13.2 [resilience] F7: fill `internal/healthcheck` (startup round, interval, probe timeout `min(interval, timeout)`, threshold, recovery, no effect on breaker or limits); verify fake-clock and `httptest` unit tests
- [x] 13.3 [mock] Write e2e `tests/retry_test.go`, `breaker_test.go`, `health_test.go` and `balance_test.go`; verify `scripts/e2e.sh` passes on the sample, variant B and variant C once 13.1 and 13.2 merge. They cover:
  - **Retry, sample:** waits of 1s and 2s via `X-Mock-Fail-Times`.
  - **Retry, variant C:** `/c/retry` waits 1s, 2s and 4s.
  - **Retry, variant B:** `POST :3003/__mock/default {"status":500}`, send until a request starts on 3003, then assert it was retried on 3004 after at least 100ms and returned 200.
  - **Breaker, variant C:** trips at 3, returns the exact 503 body with `Retry-After`, runs the half-open trial, recovers, and restores state.
  - **Health, variant C:** a target goes unhealthy via `/__mock/health`, then recovers.
  - **Balance:** 8 sample `/api/products` requests give 6 on `products-a` and 2 on `products-b`, counted from mock logs.
- [x] 13.4 [harness] Add the host-side dead-target check to `scripts/ci.sh` per design D11; verify 502s, then all `/c/health` traffic on `products-a` within 3s, then recovery after `docker compose start products-b`
- [ ] 13.5 [orchestrator] Merge groups 9 and 13 and write `gates/M3.md` (retry timings, breaker states, 3:1 counts, health transitions); verify `scripts/ci.sh` passes

## 14. M4 transforms (F4, F6)

- [x] 14.1 [transform] F4: fill `internal/middleware/reqtransform` (400 `invalid_json_body`, trailing data rejected, `Content-Length` recalculated); verify unit tests
- [x] 14.2 [transform] F6: fill `internal/middleware/resptransform` (acts only when `FromUpstream` is true, `Accept-Encoding: identity`, JSON `Content-Type` and `Content-Length`); verify unit tests, including an upstream 503 followed by a timeout giving the plain 504 body
- [x] 14.3 [yaml] Write e2e `tests/transform_request_test.go` and `tests/transform_response_test.go`; verify `scripts/e2e.sh` passes on the sample and variant B once 14.1 and 14.2 merge. They cover:
  - **Requests:** sample `/api/legacy`, comparing the client request with what the mock received.
  - **Responses:** the envelope and header changes; `Server` and `X-Powered-By` removed; an upstream 5xx wrapped with its status kept; `X-Mock-Body` giving a raw-string `$body`; gateway errors not wrapped.
  - **Variant B:** the nested envelope, byte for byte.
- [ ] 14.4 [orchestrator] Merge groups 10 and 14 and write `gates/M4.md` with side-by-side request and response pairs; verify `scripts/ci.sh` passes

## 15. M5 finish (G2, G3)

- [x] 15.1 [harness] G2: write `.github/workflows/ci.yml` to run `scripts/ci.sh`; verify `scripts/ci.sh` exits 0 locally across all four configs
- [ ] 15.2 [orchestrator] G3: write `README.md` (how to run, configs, config spec §14 decisions, design D12, `rate_limit: false`); verify every command in it runs as written
- [ ] 15.3 [orchestrator] Run every gate, write `gates/M5.md`, and open the pull request per the apply guidance; verify CI passes on it

## Workflow follow-up

- Archive the change after the pull request merges, per the archive guidance.
