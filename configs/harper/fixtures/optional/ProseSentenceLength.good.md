The scheduler reads the queue and picks the oldest job whose lease has expired, then assigns it to the next idle worker before it returns.

The scheduler reads the queue, picks the oldest job whose lease has expired, assigns it to the next idle worker, writes the new lease to the database, records the assignment in the audit log, and then notifies the worker over the control channel before it returns to the loop.

The hook reads the file.
It reports each finding.
It exits with status one when it finds any.
The caller reads the status and decides whether to retry the write.

> The proxy keeps one connection open per upstream host.
>
> It closes the connection after thirty seconds without traffic, so a restarted upstream gets a fresh handshake on the next request.

## Connection Handling and Upstream Retry Policy for the Proxy

The proxy retries each request once and then returns the upstream error to the client unchanged.

- The importer reads every row of the source table.
- It converts each value to the target type and writes the rows in batches.
- It logs the batch count when it finishes the import.

feat(proxy): retry each request once before returning the error

The proxy now retries a failed request once before it returns the
upstream error to the client.

Co-authored-by: Pat Doe <pat@example.com>
Signed-off-by: Pat Doe <pat@example.com>

| Column  | Meaning                                                                                                                                 |
| ------- | --------------------------------------------------------------------------------------------------------------------------------------- |
| `retry` | The number of times the proxy retries each request before it returns the upstream error to the client unchanged, which defaults to one. |

<details>
<summary>Why the proxy retries once</summary>

A second retry doubles the latency of a failing request and rarely succeeds.
</details>
