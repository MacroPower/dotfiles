> [!NOTE]
> The cache holds one entry per key.

The scheduler places each pod on a node.

> [!WARNING]
> Do not delete the volume while the pod runs. The pod loses its data.

The lease lasts thirty seconds.

**Note:** The scheduler renews the lease to make sure it stays valid.

The importer reads one row at a time.

> [!NOTE]
> The importer skips blank rows.

```sh
task switch
```

> [!TIP]
> Use `--dry-run` to preview the plan.

The build runs each step in order.

**Note:** The tool prints "Done!" always, even when a step fails.

The runner keeps a log of each step.

> [!NOTE]
> The installer asks “Overwrite the file?” always, even when the file is empty.

The installer writes one file per run.

> [!NOTE]
> The sync job prints ‘Up to date.’
> never more than once a day.

The tool reports each step.

**Note:** The tool prints 'Done!' always, even when a step fails.
