# Run a sync from the browser

Refresh one pack from the web UI, check the diff, then open the pull request for real.

```bash
icon-sets-sync serve        # or: make serve
```

1. Open the address it prints (it carries the sign-in token).
2. On **Packs**, choose the pack. Its page shows the vendored and upstream versions.
3. Under **Sync**, leave **Dry run** selected and press **Start sync**. The run page streams each
   stage and the log; the refreshed files land in the pack's working tree, and nothing is committed.
4. Review the change in the pack repository (`git -C ../icon-sets-tabler diff --stat`).
5. Back on the pack page, choose **Real sync**, press **Start sync**, type the pack name to confirm,
   and confirm again. The run stamps the version, commits to `chore/sync-upstream-<version>`, pushes
   and opens the pull request; its address is in the log.

A run can be stopped with **Cancel**: the stage in flight stops and nothing after it runs. See
[`serve`](../tools/serve.md) for the flags, the safety model and where runs are kept.

---

[← Docs index](../../README.md#documentation)
