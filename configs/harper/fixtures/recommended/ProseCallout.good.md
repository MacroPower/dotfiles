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
