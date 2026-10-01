The scheduler retries the job after a transient failure.
It stops when the retry budget is spent, the queue is paused, or the job is terminal.
The scheduler retries the job after a transient failure, stops when the retry budget is spent or the queue is paused, and marks the job terminal after the last attempt.
