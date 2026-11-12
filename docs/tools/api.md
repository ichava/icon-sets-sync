# REST API

The REST API is 22 endpoints under `/api/v1` on the `serve` listener, for a script, a CI job or
another service: off until `api.enabled`, reached with a bearer token and nothing else, and never
able to do more than the web UI.

## Turning it on

```bash
icon-sets-sync api token create --name ci --scope read,sync --expires 30d   # prints the token once
ICHAVA_API_ENABLED=1 icon-sets-sync serve --no-open                          # or set api.enabled
curl -s -H "Authorization: Bearer $ICON_SETS_SYNC_TOKEN" http://127.0.0.1:7878/api/v1
```

`api.enabled` is read when `serve` starts. On a bind beyond loopback the API also needs
`api.allow_remote`, and the listener is TLS (see [Configuration](../configuration.md#tls-beyond-loopback)).

## Tokens

| Command | Does |
|---|---|
| `api token create --name <slug> --scope <list> [--expires 30d]` | makes a token and prints it once |
| `api token list` | name, ID, scopes, state (active, expired, revoked), expiry and last use |
| `api token revoke <name or id>` | revokes it; a running server refuses it from its next request |

A token is `iss_` and 43 base64url characters. Only its SHA-256 is kept, in `api-tokens.json` in the
state directory (mode `0600`); a store another user can read is refused. A presented token is
hashed and compared with every stored hash in constant time. Each accepted use is recorded as the
token's last use, written at most once a minute.

| Scope | Allows |
|---|---|
| `read` | every `GET` except the admin ones |
| `sync` | start, dry-run and cancel runs, and start an upstream check |
| `config:write` | save or reset a setting in the global config |
| `admin` | the token list, webhook deliveries and tests, stopping the server, and every other scope |

A token without the scope an endpoint needs is answered 403, naming the scope.

## Endpoints

| Method and path | Scope | What it does |
|---|---|---|
| `GET /api/v1` | read | version, the surfaces that are on, this token's name and scopes, the event types |
| `GET /api/v1/openapi.json` | read | the OpenAPI 3.1 description of all of this |
| `GET /api/v1/schemas/{type}` | read | the JSON Schema of an event type's `data` |
| `GET /api/v1/packs` | read | every pack with the last upstream check, paged (`page`, `per_page`) |
| `GET /api/v1/packs/{slug}` | read | one pack and its notes |
| `POST /api/v1/check` | sync | start an upstream check (202; it runs in the background) |
| `POST /api/v1/packs/{slug}/sync` | sync | sync one pack: a **dry run** unless `"dry_run": false` |
| `POST /api/v1/sync` | sync | sync every pack, or `"packs": [...]`: a dry run unless `"dry_run": false` |
| `GET /api/v1/jobs` | read | runs, newest first, paged; `pack` and `status` filter |
| `GET /api/v1/jobs/{id}` | read | one run and each pack's outcome |
| `GET /api/v1/jobs/{id}/log` | read | the run as JSON Lines |
| `POST /api/v1/jobs/{id}/cancel` | sync | cancel a running run |
| `GET /api/v1/events` | read | the event stream, when `events.stream` is on ([Events](events.md)) |
| `GET /api/v1/settings` | read | every setting with its value, origin, default and variable |
| `PUT /api/v1/settings/{name}` | config:write | `{"value": ...}` saved to the global config, checked as the Settings page checks it |
| `DELETE /api/v1/settings/{name}` | config:write | remove it from the global config |
| `GET /api/v1/validate` | read | check every pack checkout (`?parse=1` parses every SVG) |
| `GET /api/v1/audit/{id}` | read | a finished manifest-pack audit's set manifest |
| `GET /api/v1/webhooks/deliveries` | admin | the newest delivery records and each endpoint's state |
| `POST /api/v1/webhooks/{name}/test` | admin | send `webhook.test` to one endpoint now |
| `GET /api/v1/tokens` | admin | every token, without its hash |
| `POST /api/v1/server/stop` | admin | stop the server, as the Stop server action does |

A sync's body is optional; every field defaults:

```json
{ "dry_run": true, "force": false, "concurrency": 0, "packs": ["icon-sets-tabler"] }
```

An unknown field is refused rather than ignored, so a misspelt `dry_rnu` cannot become a real run.
An `Idempotency-Key` header (up to 255 characters) makes a retry safe: the same key from the same
token within 24 hours answers 200 with the run it started, and starts nothing. A pack already being
synced answers 409 with the run's `job_id`.

## Errors and limits

Every error is an [RFC 9457](https://www.rfc-editor.org/rfc/rfc9457) problem document,
`application/problem+json`:

```json
{ "type": "urn:icon-sets-sync:problem:insufficient-scope", "title": "Forbidden", "status": 403,
  "detail": "This endpoint needs a token with the sync scope.", "instance": "/api/v1/sync" }
```

| Limit | Value |
|---|---|
| request body | 1 MiB (413 above it) |
| rate | `api.rate_limit` requests a minute per token, default 60, with a burst of the same size (429 and `Retry-After` above it) |
| a token in the URL | refused (400): send it in the `Authorization` header |

## Safety

- **Two doors, two locks.** A bearer token opens `/api/` and nothing else; the browser's session
  cookie and the launch token open the pages and never `/api/`. The API sends no CORS header, so a
  page on another origin cannot read an answer.
- **`--read-only`** registers no endpoint that runs a sync or writes a file: they answer 405.
- **The Host allowlist** applies as it does to the pages.
- **Audit.** Every write (`POST`, `PUT`, `DELETE`) appends `{time, token, method, path, route, status}`
  to `api-audit.jsonl` in the state directory. The token's name is recorded, never the token.
- **No secret in an answer.** Settings hold none (tokens and webhook secrets live in the state
  directory), and the token list carries no hash.

## OpenAPI

`GET /api/v1/openapi.json` serves `src/internal/web/openapi.json`, an OpenAPI 3.1 document where
`x-scope` names each operation's scope. The routes are declared in `apiRoutes`, one per line; the
document describes them and is not their source. `TestOpenAPIMatchesTheRoutes` reads the
declarations and fails when a route is missing from the document, the document names a route that
does not exist, or the two disagree on a scope.

---

[← Docs index](../../README.md#documentation)
