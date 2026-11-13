# Events

Eleven event types, each a CloudEvents 1.0 envelope in the JSON format with a JSON Schema for its
`data`, published on one in-process bus that the API's event stream and the webhooks read.

## The envelope

Every event is a [CloudEvents 1.0](https://github.com/cloudevents/spec/blob/v1.0.2/cloudevents/spec.md)
envelope in the [JSON format](https://github.com/cloudevents/spec/blob/v1.0.2/cloudevents/formats/json-format.md),
so a receiver can use the tooling that format already has:

```json
{
  "specversion": "1.0",
  "id": "01JCXQ7Z3V8M2N5K9T4R6W1Y0B",
  "source": "icon-sets-sync://build-01",
  "type": "job.finished",
  "time": "2026-11-12T10:04:31.512Z",
  "datacontenttype": "application/json",
  "data": { "job_id": "20261112T100412Z-3fa2c1", "kind": "sync", "packs": ["icon-sets-tabler"],
            "dry_run": true, "status": "succeeded", "duration_ms": 19104 }
}
```

`id` is a [ULID](https://github.com/ulid/spec): unique, and sortable by time. `source` names the
machine. A webhook delivery uses `id` as its `webhook-id`, the same on every retry, so a receiver
can drop a repeat.

## Types

| Type | When | `data` |
|---|---|---|
| `job.started` | a run began, from the web UI or the API | `job_id`, `kind`, `packs`, `dry_run`, `label` |
| `job.progress` | a pipeline stage started, finished or failed (**stream only**) | `job_id`, `pipeline`, `stage`, `state`, `error` |
| `job.finished` | a run ended | `job_id`, `kind`, `packs`, `dry_run`, `status`, `error`, `duration_ms` |
| `pack.synced` | one pack of a run refreshed from upstream (a dry run too) | `job_id`, `pack`, `dry_run`, `detail` |
| `pack.behind_upstream` | an upstream check found a pack behind | `pack`, `current`, `latest` |
| `upstream.checked` | an upstream check of every pack finished | `packs`, `stale`, `errored`, `error` |
| `config.changed` | a config file or setting was saved | `file`, `key`, `action` (never the value) |
| `server.started` | `serve` began answering | `version`, `address`, `tls`, `read_only`, `api` |
| `server.stopping` | `serve` is shutting down | `reason`: `signal`, `stop-action` or `error` |
| `webhook.disabled` | an endpoint was turned off after 50 consecutive failures | `name`, `consecutive_failures` |
| `webhook.test` | a test delivery | `name`, `message` |

`job.progress` is too frequent for a webhook, so an endpoint cannot subscribe to it. Runs started by
`icon-sets-sync mcp` run in that process and publish nothing on a server's bus.

## Schemas

Each type's schema is embedded in the binary (`src/internal/events/schemas/<type>.json`) and served
at `GET /api/v1/schemas/<type>`, and to an MCP client as the resource
`icon-sets-sync://schemas/events/<type>`. Two tests hold the list honest:
`TestEveryTypeHasASchema` fails on a type with no schema or a schema with no type, and
`TestEveryTypeHasAPublisher` fails on a type no production code publishes.
`TestRunEventsMatchTheirSchemas` validates every event a run publishes against its schema.

## The stream

With `api.enabled` and `events.stream`, `GET /api/v1/events` is a
[Server-Sent Events](https://html.spec.whatwg.org/multipage/server-sent-events.html) stream of the
bus. Each message's `id` is the event's ID, its `event` the type, and its `data` the whole envelope.
`?type=` (repeatable) keeps only those types:

```bash
curl -N -H "Authorization: Bearer $ICON_SETS_SYNC_TOKEN" \
  "http://127.0.0.1:7878/api/v1/events?type=job.finished&type=pack.behind_upstream"
```

There is no replay: an event happens once, and a client that reconnects sees what happens next. A
client too slow to keep up loses events past its 1,000-event queue rather than slowing a sync down.
The per-run stream the web UI reads (`/events/jobs/{id}`) is separate and unchanged: it replays a
run from any point with `Last-Event-ID`.

---

[← Docs index](../../README.md#documentation)
