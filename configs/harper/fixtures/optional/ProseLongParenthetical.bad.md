The pool (which the scheduler sizes once at startup from the node count) holds the workers.

Each worker reads the queue (or the backlog, when the queue is empty and the flag is set) before it exits.
