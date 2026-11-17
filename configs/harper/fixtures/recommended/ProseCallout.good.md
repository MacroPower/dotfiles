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

The flag reads one directory.

**Note:** The flag takes a directory, e.g. never the root of the repository.

The cache keys each entry by path.

> [!NOTE]
> The cache stores each path in one form, i.e. always the absolute path.

The summary lists the changes of each run.

**Note:** The summary shows add vs. remove counts for each run.

The parser reads each date in the log.

**Note:** The parser stores each date in UTC (cf. update times in the audit log, which stay local).

The installer waits for the lock.

**Note:** The installer prints Waiting... always, even when the lock is free.

The lock file guards the cache.

**Note:** 'Keep in mind that the lock stays,' the guide says.

The guide describes the lock.

**Note:** "Keep in mind that the lock stays," the guide says.

The lock outlives the run.

**Note:** ‘Keep in mind that the lock stays,’ the guide says.

The migration rewrites the cache.

<!-- prettier-ignore-start -->
Note: The cache holds the last response.
## Run the Migration
<!-- prettier-ignore-end -->

Tip: The migration takes a minute.

The migration rewrites the cache.

> [!NOTE]
>
> ## Run the Migration
>
> The migration takes a minute.
