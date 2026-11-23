# REST API

Base URL: wherever `HTTP_ADDR` serves (default `http://localhost:8080`). All API routes are under `/api`; the same paths without `/api` serve the HTML web UI. Responses are JSON. List endpoints paginate with `cursor` and `limit`.

> This documents the intended MVP surface. The authoritative reference is your build's README and code — if a request here is rejected, check there and treat the code as truth.

## Health & stats

**Example:** Check system health and ingest statistics:

```bash
curl http://localhost:8080/health
curl http://localhost:8080/api/stats
```

Sample response payload for `GET /api/stats`:

```json
{
  "total_events": 14200,
  "contract_count": 85,
  "first_ledger": 500000,
  "last_ledger": 512000,
  "approximate": false
}
```

### `GET /health`
Reports overall health and, in a `dependencies` array, each backend component's own status. Standalone mode reports `postgres` and `stellar_rpc`; upstream mode reports only `sorotrail`, because it opens no database. Each dependency carries `healthy`, a `severity` of `fatal` (the read paths break without it) or `degraded` (some data is still served), and a sanitized `detail` when unhealthy. The existing top-level fields are unchanged, and `/readyz` returns `200`/`503` under the same conditions as before. Liveness (`/livez`) deliberately does not check dependencies.

```bash
curl http://localhost:8080/health
```

Sample response (`200 OK`):

```json
{
  "mode": "rpc",
  "healthy": true,
  "latest_ledger": 512000
}
```

### `GET /api/stats`
Overview counters: total contracts seen, total events, last-updated ledger (standalone mode) or last-synced position (upstream mode).

## Contracts

### `GET /api/contracts`
List every known contract, newest-activity-first.

**Query parameters:** `search` (partial contract ID match), `limit`, `cursor`.

```bash
curl 'http://localhost:8080/api/contracts?limit=20'
```

### `GET /api/contracts/{id}`
Summary for one contract: event count, first/last seen ledger, event-type breakdown.

## Events

### `GET /api/contracts/{id}/events`
A contract's events, newest-first.

**Query parameters:**

| Param | Description |
|---|---|
| `type` | `contract`, `system`, or `diagnostic` |
| `tx_hash` | Restrict to events from one transaction |
| `topic` | Match by decoded topic (see build semantics) |
| `from_ledger` / `to_ledger` | Inclusive ledger bounds |
| `from_time` / `to_time` | Inclusive RFC 3339 bounds on `ledger_closed_at` (on-chain close time, not ingest time) |
| `limit` / `cursor` | Pagination |

```bash
curl 'http://localhost:8080/api/contracts/CC.../events?type=contract&limit=50'
```

### `GET /api/events/{id}`
Fetch a single event by its unique id (TOID), fully decoded.

### `GET /api/events/stream`
Stream newly ingested events in real time over Server-Sent Events (`text/event-stream`).

**Query parameters:** Accepts the same filters as `GET /api/events`: `contract_id`, `type` (`contract`, `system`, `diagnostic`), `topic`, `tx_hash`, `from_ledger`, and `to_ledger`.

```bash
curl -N 'http://localhost:8080/api/events/stream?contract_id=CDLZFC3SYJYDZT7K67VZ75HPJVIEUVNIXF47ZG2FB2RMQQVU2HHGCYSC&type=contract'
```

**Frame format & heartbeats:**
- An immediate `:hb` comment frame is flushed upon connection so clients receive HTTP 200 headers right away, followed by `:hb` comments every 15 seconds to keep idle connections alive through reverse proxies.
- Matching events are emitted as `event: event` frames with `id: <TOID>` and `data: <Event JSON>`.

**Backpressure & delivery guarantees:**
- Delivery is **best-effort** and live-only (no database connection is held per subscriber).
- Each subscriber has a bounded in-memory channel of **256 events**. The ingester's `Publish` path is non-blocking: if a slow or stalled client fills its 256-event buffer, the broker closes that subscriber's stream and removes the subscription rather than stalling ingestion or growing memory without bound.
- Clients that disconnect or are dropped for backpressure can catch up without gaps by querying `GET /api/events` with a `cursor` or `from_ledger`.
- **Upstream mode (`SOURCE_MODE=sorotrail`):** Because upstream mode has no local ingester, `GET /api/events/stream` explicitly returns `501 Not Implemented` rather than holding open a silent stream.

### `GET /api/events/buckets` and `GET /api/contracts/{id}/buckets`
Aggregate event counts into contiguous half-open `[start, end)` time buckets over a `[from, to)` RFC3339 timestamp range.

- **Timestamp bucketed:** `ledger_closed_at` (the on-chain ledger close time, not `created_at` ingest time, so backfilled history is placed at its true on-chain time).
- **Half-open boundaries:** Each bucket covers `[start, end)` (`start <= ledger_closed_at < end`). An event whose `ledger_closed_at` equals a bucket boundary lands in the bucket starting at that boundary; an event at `to` is excluded.
- **Empty buckets:** Quiet intervals within `[from, to)` are returned with `"count": 0` rather than omitted.
- **Bounds & cap:** `interval` defaults to `1h` and accepts durations between `1m` and `168h` (`7d`), such as `1m`, `5m`, `15m`, `1h`, `6h`, `1d`. A single request may produce at most `500` buckets (`400 Bad Request` when `(to - from) / interval > 500`).
- **Upstream mode (`SOURCE_MODE=sorotrail`):** Buckets are derived from the bounded recent-event scan and report `"approximate": true` whenever the scan does not exhaust the upstream history.

**Query parameters:**

| Param | Description |
|---|---|
| `from` | Required inclusive lower bound (RFC3339 timestamp) |
| `to` | Required exclusive upper bound (RFC3339 timestamp) |
| `interval` | Bucket width (`1m` .. `7d`, default `1h`; max 500 buckets) |
| `contract_id` | Optional Soroban contract strkey (`GET /api/events/buckets` only) |
| `type` | Optional `contract`, `system`, or `diagnostic` |
| `topic` | Optional decoded topic filter (bare symbol or JSON object) |

```bash
curl 'http://localhost:8080/api/events/buckets?from=2024-01-01T00:00:00Z&to=2024-01-01T06:00:00Z&interval=1h&type=contract'
```

## Event responses and the token annotation

Event objects returned by `GET /api/events`, `GET /api/contracts/{id}/events`, `GET /api/events/{id}` and the SSE stream may carry a `token` field describing the event as a SEP-41 token operation. It names the event and resolves its topics into `from`, `to` and `amount` slots. See [SEP-41 token annotation](token-events.md) for the recognised events, the per-event slot meanings and the caveat that the field describes an event's shape rather than verifying that a contract implements SEP-41.

## Pagination

List responses include `next_cursor`; pass it back as `cursor` for the next page. A null `next_cursor` means you've reached the end. Treat the cursor as opaque.

## Rate limiting

Rate limiting is **off by default**. Set both `RATE_LIMIT_RPS` (sustained requests per second) and `RATE_LIMIT_BURST` (instantaneous allowance) to enable a per-client token bucket over `/api`. A partial pair is rejected at startup rather than guessed at.

When a client exceeds its allowance the response is `429 Too Many Requests` with a `Retry-After` header in whole seconds and the usual JSON error envelope:

```json
{
  "error": "rate limit exceeded; retry after 2 seconds",
  "code": "Too Many Requests",
  "request_id": "..."
}
```

`Retry-After` comes from the limiter's actual token state, so it is the real wait until capacity returns rather than a fixed constant.

**Key:** the socket's remote address. Behind a proxy, set `RATE_LIMIT_TRUSTED_PROXY=true` to key on the first `X-Forwarded-For` entry instead — only safe when the proxy overwrites that header, since a client can otherwise choose its own bucket.

**Exempt:** `/health`, `/livez`, `/readyz`, `/metrics`, and the web UI (its htmx partials are ordinary page loads and a limit tuned for API clients must not lock out a browsing session). Only `/api` is limited.

Rejections increment `sorovista_http_rate_limited_total`, so an operator can tell a tight limit from real abuse.

## Errors

Errors return an envelope with an HTTP status matching the failure class — `400` for invalid input (naming the offending parameter), `404` for an unknown contract or event, `500` for server faults. In upstream mode, an unreachable SoroTrail instance surfaces as a `502`-class error rather than a silent empty result.
