The scheduler retries the job after a transient failure.
It stops when the retry budget is spent, the queue is paused, or the job is terminal.

The scheduler reads the queue, picks the oldest job whose lease has expired, assigns it to the next idle worker, and writes the new lease to the database before it returns.

| Column  | Meaning                                                                                                                                                  |
| ------- | -------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `retry` | The number of times the proxy retries each request before it returns the upstream error to the client unchanged, which defaults to one and caps at five. |

<!-- prettier-ignore -->
Host|Notes
:--|:--
Apache|The server reads the file at startup and serves it for each missing page
Caddy|The server reads the directive at startup and serves the page for each miss
Firebase|The host reads the config at deploy time and serves the page for each miss
GitHub Pages|The host serves a file named 404.html from the root of the site

<!-- prettier-ignore -->
| Host | Notes |
| :-- | :-- |
Apache|The server reads the file at startup and serves it for each missing page
Caddy|The server reads the directive at startup and serves the page for each miss
Firebase|The host reads the config at deploy time and serves the page for each miss
GitHub Pages|The host serves a file named 404.html from the root of the site

<!-- prettier-ignore-start -->
The scheduler hands each job to one of these hosts, and each host serves the error page for a missing path in its own way:
Host|Notes on how the host serves the page for a missing path
:--|:--
Apache|The server reads the file at startup and serves it for each missing page
<!-- prettier-ignore-end -->

<!-- prettier-ignore -->
Flag|Effect
:--|:--
retry|The proxy retries each failed request once with a short delay, and then it returns the upstream error to the client unchanged, which keeps the caller from waiting on a dead host for long

<!-- prettier-ignore -->
Badge|Meaning
:--|:--
<img src="ok.svg" alt="ok">|The build passed
Nightly|The nightly job ran on the main branch and pushed the image to the registry
Release|The release job tagged the commit and wrote the notes from the merged pull requests
Docs|The docs job built the site and published it to the pages branch of the repository
