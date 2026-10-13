The scheduler retries the job after a transient failure.
It stops when the retry budget is spent, the queue is paused, or the job is terminal.

The scheduler reads the queue, picks the oldest job whose lease has expired, assigns it to the next idle worker, and writes the new lease to the database before it returns.

| Column  | Meaning                                                                                                                                                  |
| ------- | -------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `retry` | The number of times the proxy retries each request before it returns the upstream error to the client unchanged, which defaults to one and caps at five. |
