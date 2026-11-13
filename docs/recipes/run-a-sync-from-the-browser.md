# Run a sync from the browser

Refresh one pack from the web UI, check the diff, then open the pull request for real.

```bash
icon-sets-sync serve        # or: make serve
```

1. Open the address it prints (it carries the sign-in token).
2. On **Packs**, press the row's sync button to open the sync panel, or open the pack's page. The
   page shows the vendored and upstream versions, and **CLI & CI** slides in the same actions as
   commands.
3. Leave **Dry run** selected and press **Start sync**. The run page streams each stage and the log,
   with the counts of SVGs that would be added, removed and changed. A dry run writes nothing into the
   pack.
4. Back on the pack page, choose **Real sync**, press **Start sync**, type the pack name, and confirm
   in the dialog. The run stamps the version, commits to `chore/sync-upstream-<version>`, pushes and
   opens the pull request; its address is in the log.

A run can be stopped with **Cancel**, after a confirm dialog: the stage in flight stops and nothing
after it runs. See
[`serve`](../tools/serve.md) for the flags, the safety model and where runs are kept.

---

[← Docs index](../../README.md#documentation)
