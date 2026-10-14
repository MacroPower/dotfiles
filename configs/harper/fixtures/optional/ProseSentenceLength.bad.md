The scheduler reads the queue, picks the oldest job whose lease has expired, assigns it to the next idle worker, and writes the new lease to the database before it returns.

- The importer reads every row of the source table, converts each value to the target type, and writes the rows in batches of one hundred to the destination table.

> The proxy keeps one connection open per upstream host, and it closes the connection after thirty seconds without traffic so that a restarted upstream gets a fresh handshake.

The parser accepts every common archive format, e.g. tar, zip, and gzip, and it streams each entry to the caller without writing a temporary file to the disk.

The scheduler retries a failed job three times with a growing delay between attempts, and the last failed attempt marks the job as dead for the operators on call.

The proxy keeps one open connection to each upstream host that it contacted in the last minute, which gives a restarted upstream a fresh handshake on its next request.
