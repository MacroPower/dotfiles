The scheduler reads the queue, picks the oldest job whose lease has expired, assigns it to the next idle worker, and writes the new lease to the database before it returns.

- The importer reads every row of the source table, converts each value to the target type, and writes the rows in batches of one hundred to the destination table.

> The proxy keeps one connection open per upstream host, and it closes the connection after thirty seconds without traffic so that a restarted upstream gets a fresh handshake.

The parser accepts every common archive format, e.g. tar, zip, and gzip, and it streams each entry to the caller without writing a temporary file to the disk.

The scheduler retries a failed job three times with a growing delay between attempts, and the last failed attempt marks the job as dead for the operators on call.

The proxy keeps one open connection to each upstream host that it contacted in the last minute, which gives a restarted upstream a fresh handshake on its next request.

The proxy reads its routes from https://example.com/routes; it caches each route for one minute and reloads the whole file from the disk whenever the operator sends it a signal.

The proxy reads the list of its routes from https://example.com/routes: it caches each route for one minute and reloads the whole file whenever the operator sends it a signal.

The importer prints "Batch done!" each time it writes a batch to the destination table, but it still keeps the rows of the last batch in memory until it exits.

The importer prints “Batch done!” _each_ time it writes a batch to the destination table, but it still keeps the rows of the last batch in memory until it exits.
