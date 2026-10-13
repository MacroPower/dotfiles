The scheduler retries the job whenever the worker reports a transient failure and the retry budget for that job has not been spent yet and the queue is not paused by an operator and the job is not marked as terminal.

The scheduler reads the queue, picks the oldest job whose lease has expired, assigns it to the next idle worker, writes the new lease to the database, and records the assignment in the audit log.

- The importer reads every row of the source table, converts each value to the target type, checks it against the schema, and writes the rows in batches of one hundred to the destination table.
