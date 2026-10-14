The scheduler retries the job whenever the worker reports a transient failure and the retry budget for that job has not been spent yet and the queue is not paused by an operator and the job is not marked as terminal.

The scheduler reads the queue, picks the oldest job whose lease has expired, assigns it to the next idle worker, writes the new lease to the database, and records the assignment in the audit log.

- The importer reads every row of the source table, converts each value to the target type, checks it against the schema, and writes the rows in batches of one hundred to the destination table.

The scheduler retries a failed job three times with a growing delay between one attempt and the next one, but the last failed attempt marks the job as dead for the operators on call.

The proxy keeps one open connection to each upstream host that it contacted during the last minute of traffic, which gives a restarted upstream a fresh handshake on the next request it receives.
