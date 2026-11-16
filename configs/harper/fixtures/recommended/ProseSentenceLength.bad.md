The scheduler retries the job whenever the worker reports a transient failure and the retry budget for that job has not been spent yet and the queue is not paused by an operator and the job is not marked as terminal.

The scheduler reads the queue, picks the oldest job whose lease has expired, assigns it to the next idle worker, writes the new lease to the database, and records the assignment in the audit log.

| Key | Value | <!-- same finding -->
| ---- | ----- | <!-- same finding -->
| port | open | <!-- same finding -->

The proxy reads the route table, picks the upstream host with the fewest open connections, opens a new connection to it, forwards the request with its headers, and logs the choice to stdout|stderr.

<!-- prettier-ignore-start -->
| Key | Value | <!-- same finding -->
| ---- | ----- | <!-- same finding -->
| port | open | <!-- same finding -->
The proxy reads the route table, picks the upstream host with the fewest open connections, opens a new connection to it, forwards the request with its headers, and records the choice in the access log.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
| Key | Value | <!-- same finding -->
| ---- | ----- | <!-- same finding -->
> The proxy reads the route table, picks the upstream host with the fewest open connections, opens a new connection to it, forwards the request with its headers, and logs the choice to stdout|stderr.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
The scheduler hands each job to one of these hosts, picks the host with the fewest open connections, and then lets each host serve the error page for a missing path in its own way:
Host|Notes <!-- same finding -->
:--|:-- <!-- same finding -->
Apache|The server reads the file at startup and serves it for each missing page <!-- same finding -->
<!-- prettier-ignore-end -->

- The importer reads every row of the source table, converts each value to the target type, checks it against the schema, and writes the rows in batches of one hundred to the destination table.

The scheduler retries a failed job three times with a growing delay between one attempt and the next one, but the last failed attempt marks the job as dead for the operators on call.

The proxy keeps one open connection to each upstream host that it contacted during the last minute of traffic, which gives a restarted upstream a fresh handshake on the next request it receives.

The proxy lets the browser load every script and every image of the site that the pattern
https://example.com/* covers, and it blocks every request to the other hosts of the network in the default policy. <!-- same finding -->
