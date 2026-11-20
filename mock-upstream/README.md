# mock-upstream
Env: `SERVICE_NAME` (mock), `PORT` (3000), `HEALTH_PATH` (/healthz), `EXTRA_RESPONSE_HEADERS` (`Name=value;Name=value`). Every response carries `X-Mock-Service` and the extra headers.
Default: any method, any path returns 200 JSON `{service,method,path,query,headers,body}`. Header names lowercase; multi-values joined `, `. Body is JSON if valid, else a string, `null` if empty.
Request headers (ignored on `HEALTH_PATH`; they override `/__mock/default`):
- `X-Mock-Status: 503` respond with that status (200-599). `X-Mock-Delay-Ms: 500` wait first. `X-Mock-Body: text` plain-text body.
- `X-Mock-Fail-Times: 2` + `X-Mock-Key: abc` first 2 with key abc fail (`X-Mock-Status` or 503), later get 200. `X-Mock-Drop: 1` close the socket, no response.
Admin (not logged; 400 on bad JSON, 405 on wrong method):
- `GET /__mock/requests` array of `{method,path,uri,query,headers,body,time}` in arrival order; `time` is receipt (before delay). `[]` when empty.
- `POST /__mock/reset` clears log, fail counters, health and default overrides. `POST /__mock/health {"status":503}` sets `HEALTH_PATH` status.
- `POST /__mock/default {"status":503,"delay_ms":0}` applies to every non-admin, non-health request until reset. `HEALTH_PATH` is never logged.
