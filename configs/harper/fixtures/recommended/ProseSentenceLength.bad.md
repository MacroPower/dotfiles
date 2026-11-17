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

<!-- prettier-ignore-start -->
- The importer reads every row of the source table, converts each value to the target type, checks it against the schema, and writes the rows in batches of one hundred to the destination table.
--- <!-- same finding -->
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
- The scheduler starts one worker for each job. <!-- same finding -->

  The importer reads every row of the source table, converts each value to the target type, checks it against the schema, and writes the rows in batches of one hundred to the destination table.
--- <!-- same finding -->
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
> The proxy keeps one open connection to each upstream host that it contacted during the last minute of traffic, which gives a restarted upstream a fresh handshake on the next request it receives.
=== <!-- same finding -->
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
The importer reads every row of the source table, converts each value to the target type, checks it against the schema, and writes the rows in batches of one hundred to the destination table.
*** <!-- same finding -->
Next steps <!-- same finding -->
========== <!-- same finding -->
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
The proxy keeps one open connection to each upstream host that it contacted during the last minute of traffic, which gives a restarted upstream a fresh handshake on the next request it receives.
> Note <!-- same finding -->
=== <!-- same finding -->
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
The importer reads every row of the source table, converts each value to the target type, and then hands the rows to
**`kubectl apply`** <!-- same finding -->
which writes them in batches of one hundred to the destination table for the operators on call. <!-- same finding -->
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
The proxy reads the route table, picks the upstream host with the fewest open connections, and follows the steps of the guide at
**https://example.com/guide** <!-- same finding -->
for each request that reaches it from a client on the network. <!-- same finding -->
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
The importer reads every row of the source table and marks each row that the schema rejected with the three marks
-*- <!-- same finding -->
in the log, so the operators on call can find each of those rows later. <!-- same finding -->
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
The importer reads every row of the source table, converts each value to the target type, checks it against the schema, and then writes the rows in batches of one hundred to the directory at
--- `/var/lib/importer` <!-- same finding -->
<!-- prettier-ignore-end -->

- The importer reads every row of the source table, converts each value to the target type, checks it against the schema, and writes the rows in batches of one hundred to the destination table.

The scheduler retries a failed job three times with a growing delay between one attempt and the next one, but the last failed attempt marks the job as dead for the operators on call.

The proxy keeps one open connection to each upstream host that it contacted during the last minute of traffic, which gives a restarted upstream a fresh handshake on the next request it receives.

The proxy lets the browser load every script and every image of the site that the pattern
https://example.com/* covers, and it blocks every request to the other hosts of the network in the default policy. <!-- same finding -->

The scheduler reads the queue and then starts one worker for each job that it finds in the queue, and the guide at
*https://example.com/guide* covers the rest of the steps that the worker runs after it starts the job. <!-- same finding -->

The scheduler reads the queue and then starts one worker for each job that it finds in the queue, and the guide at
https://example.com/guide <!-- same finding -->
covers the rest of the steps that the worker runs after it starts the job. <!-- same finding -->

The importer reads every row of the source table and converts each value to the target type, and the command
`importer --dry-run` <!-- same finding -->
prints each row that it would write to the destination table without writing any of them. <!-- same finding -->
