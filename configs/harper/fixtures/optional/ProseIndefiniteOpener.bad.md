A task is a unit of work. A task runs on one worker.

The queue holds jobs. An entry that expires drops out. A lease guards each entry.

- A hook runs after every write. A second hook lints the commit message.

**Findings.** A finding names a phrase. A finding carries a tier.

A cache sits in front of the store. _A_ miss reads the store. A hit returns at once.

A reader opens one rule file at a time.

An entry expires after an hour.

A failed task can retry twice.

The worker reads `queue.toml`. A task runs on one worker.

A new worker is `idle`. One task wakes it up.

The worker reads https://example.com/queue. A task runs on one worker.

A worker reads the queue with
`queue --list` <!-- same finding -->
and picks the oldest entry. A lease is a lock on that entry.

The operator says “done.” A worker restarts each failed job.

What is cached? “A token is.” The worker reads the file again.

What is cached? ‘A token is.’ The worker reads the file again.

A user typed "`stop`." A user then left.

"A lease is held?" the guide asks.

A lease is a lock. "A lease?" the guide asks.
