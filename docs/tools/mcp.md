# MCP

`icon-sets-sync mcp` is a [Model Context Protocol](https://modelcontextprotocol.io/) server on stdio,
built on the official Go SDK (`github.com/modelcontextprotocol/go-sdk` v1.8.0), with eight read
tools and three write tools: off until `mcp.enabled`, and opening no network port.

## Turning it on

An agent's host spawns the server and talks to it over stdin and stdout. Set `mcp.enabled` in the
global config (or on the Settings page), or pass `--enable`, and point the host at the binary:

```json
{
  "mcpServers": {
    "icon-sets-sync": {
      "command": "/usr/local/bin/icon-sets-sync",
      "args": ["--config-dir", "/work/icon-sets-sync/config", "mcp", "--enable"]
    }
  }
}
```

Without either, `icon-sets-sync mcp` exits 2 and says how to turn it on. Everything but the protocol
goes to stderr.

## Tools

| Tool | Kind | What it does |
|---|---|---|
| `list_packs` | read | every pack: slug, repository, recipe and vendored version |
| `get_pack` | read | one pack and its authoring notes |
| `check_upstream` | read | poll the registries for the named packs (all when none) and report which are behind |
| `list_jobs` | read | runs, newest first: this process's and the history `serve` keeps |
| `get_job` | read | one run's status and each pack's outcome |
| `get_job_log` | read | a run's newest events |
| `validate_configs` | read | validate every config file as the loader would |
| `dry_run_sync` | read | start a dry run of one pack and return its run ID; poll `get_job` |
| `sync_pack` | write | sync one pack: a dry run unless `dry_run` is false, and then only with `confirm` set to the slug |
| `cancel_job` | write | cancel a run this process started |
| `set_setting` | write | save one setting to the global config, checked as the Settings page checks it |

The read tools are offered whenever the server is on; `dry_run_sync` writes nothing into a pack and
commits nothing. **The write tools exist only with `mcp.allow_writes`, and never with
`--read-only`**: a tool that is not registered cannot be called, however a prompt asks. A real sync
also needs `confirm` equal to the pack's slug, and is refused while a `serve` is running on the
machine, so two processes never work one pack repository at once; start it through that server's
UI or API instead. Every write tool call is recorded in `api-audit.jsonl` in the state directory,
under the name `mcp`.

`TestWriteToolsExistOnlyWithAllowWritesAndNeverReadOnly` lists the tools over the SDK's in-memory
transport for each combination; `TestARealSyncNeedsConfirmEqualToTheSlug` holds the confirmation.

## Resources

| URI | What |
|---|---|
| `icon-sets-sync://schemas/events/<type>` | each event type's JSON Schema ([Events](events.md)) |
| `icon-sets-sync://schemas/pack` | the pack config schema |
| `icon-sets-sync://schemas/global` | the global config schema |

## Runs

Runs the MCP server starts run in its own process, with the same one-run-per-pack lock, and are
saved to the same history `serve` keeps, so they appear on the web UI's Runs page. When the host
closes the server, a run in progress finishes before the process exits. They publish no events on a
running server's bus.

---

[← Docs index](../../README.md#documentation)
