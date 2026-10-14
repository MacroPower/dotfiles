The pool (which the scheduler sizes once at startup from the node count) holds the workers.

Each worker reads the queue (or the backlog, when the queue is empty and the flag is set) before it exits.

The hook, which runs after every Write and Edit of a markdown file or a source file, tags each finding.
