Pass a flag, e.g. `--force`, to skip the check.

The pair (i.e., key and value) is stored.

See the spec (cf. RFC 9110).

N.B. the cache is cold after a restart.

Smith et al. measured the latency.

<!-- prettier-ignore-start -->
> The job runs nightly, e.g. at noon.
>     Rerun it, e.g. hourly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
> - The job runs nightly, e.g. at noon.
>
>     Rerun it, e.g. hourly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
> The job runs nightly, e.g. at noon.
>     # Rerun it, e.g. hourly.
>     Rerun it, e.g. hourly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
> ####### The job runs, e.g. nightly.
>     Rerun it, e.g. hourly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
> The job runs, e.g. nightly,
and lazily, e.g. at noon.
>     Rerun it, e.g. hourly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
> > The job runs, e.g. nightly.
>     Rerun it, e.g. hourly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
> > - The job runs, e.g. nightly,
> and lazily, e.g. at noon.
> >
> >     Rerun it, e.g. hourly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
> - The job runs, e.g. nightly.
> | Rerun it, e.g. hourly. |
>
>     Rerun it, e.g. at noon.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
- > The job runs nightly, e.g. at noon.
  >     Rerun it, e.g. hourly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
- >> The job runs nightly, e.g. at noon.
  >>     Rerun it, e.g. hourly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
- The job runs, e.g. nightly,
  and reruns, e.g. hourly,
      >     and again, e.g. at noon.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
1. An item, e.g. one.
  > A quote, e.g. this one.
    > # and more, e.g. lazily.
   >     Rerun it, e.g. hourly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
> The job runs, e.g. nightly,
    - and lazily, e.g. at noon.
>     Rerun it, e.g. hourly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
- The job runs, e.g. nightly.
  - It reruns, e.g. hourly.

  The parent goes on, e.g. at noon,
      >     and again, e.g. at midnight.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
- # The job runs, e.g. nightly
The text goes on, e.g. at noon,
    >      and again, e.g. hourly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
The job shipped, e.g. in
2026. It runs, e.g. nightly,
       >     and again, e.g. hourly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
- - > The job runs, e.g. nightly,
    >     and again, e.g. hourly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
- > - The job runs, e.g. nightly.
  >
  >     It reruns, e.g. hourly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
- > - > The job runs, e.g. nightly,
  >   >     and again, e.g. hourly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
> - The job runs, e.g. nightly.
>   > - It reruns, e.g. hourly.
>
>     The job goes on, e.g. at noon.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
-   The job runs, e.g. nightly.
	- > It reruns, e.g. hourly,
      >     and again, e.g. at noon.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
1. >   - The job runs, e.g. nightly.
      >
      >       It reruns, e.g. hourly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
> > - > The job runs, e.g. nightly,
> >   >     and again, e.g. hourly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
- The job runs, e.g. nightly,
      - and reruns, e.g. hourly,
        >     and again, e.g. at noon.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
1. The job runs, e.g. nightly.
   - It reruns, e.g. hourly.
    > > - It logs, e.g. to disk,
   >   >       and again, e.g. at noon.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
1. The job runs, e.g. nightly.
   # A heading, e.g. this one
    - > It reruns, e.g. hourly,
      >     and again, e.g. at noon.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
# The job, e.g. nightly
2. It runs, e.g. at noon.
   - It reruns, e.g. hourly.
    > - > It logs, e.g. to disk,
   >     and again, e.g. at midnight.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
1234567890. The job runs, e.g. nightly,
            >     and again, e.g. hourly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
> The job runs, e.g. nightly,
1234567890. and again, e.g. hourly,
>     and once more, e.g. at noon.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
<div> The job runs, e.g. nightly.
- It reruns, e.g. hourly.

  It runs again, e.g. at noon,
     >     and once more, e.g. at midnight.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
- The job runs, e.g. nightly.

  <div> It reruns, e.g. hourly.
  - It runs again, e.g. at noon.

    It logs, e.g. to disk,
       >     and again, e.g. at midnight.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
> The job runs, e.g. nightly.
> - It reruns, e.g. hourly.
>
>     It runs again, e.g. at noon.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
> > The job runs, e.g. nightly.
> 2. It reruns, e.g. hourly.
>
>     It runs again, e.g. at noon.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
> 1. Build the job, e.g. nightly.
> 2. > It runs, e.g. at noon,
>    >     and again, e.g. hourly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
- > The job runs, e.g. nightly.
> 2. It reruns, e.g. hourly.
>
>     It runs again, e.g. at noon.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
>  3. ## The job, e.g. nightly
> 4. >    It runs, e.g. at noon,
>    >     and again, e.g. hourly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
2. The job runs, e.g. nightly.
   2. >  It reruns, e.g. hourly.
      > 10. 2026. It runs, e.g. at noon.
      >
      >     It logs, e.g. to disk.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
1. > 	> The job runs, e.g. nightly,
   > >     and again, e.g. hourly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
- > > The job runs, e.g. nightly,
and lazily, e.g. at noon.
    > - It reruns, e.g. hourly.
  >
  >      It runs again, e.g. at noon.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
<div> The job runs, e.g. nightly.
***
    It reruns, e.g. hourly.
</div> <!-- same finding -->
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
The job runs, e.g. nightly,
    ***
    and again, e.g. hourly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
- The job runs, e.g. nightly.
  ## Steps, e.g. build
  ***
    It reruns, e.g. hourly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
- The job runs, e.g. nightly.

  ## Steps, e.g. build

    It reruns, e.g. hourly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
1. The job runs, e.g. nightly.

   | Step, e.g. build | Time, e.g. noon |
   | --- | --- | <!-- same finding -->

     It reruns, e.g. hourly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
- The job runs, e.g. nightly.

  ## Steps, e.g. build

  The job runs, e.g. nightly,
and lazily, e.g. hourly.

    It reruns, e.g. hourly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
- The job runs, e.g. nightly.

  | Step, e.g. build | Time, e.g. noon |
  | --- | --- | <!-- same finding -->
  ## Steps, e.g. build
  The job runs, e.g. nightly,
and lazily, e.g. hourly.

    It reruns, e.g. hourly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
- The job runs, e.g. nightly.

  | Step, e.g. build |
and lazily, e.g. hourly.

    It reruns, e.g. hourly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
- The job runs, e.g. nightly.

  ## Steps, e.g. build

  The job runs, e.g. nightly,
         ***
and lazily, e.g. hourly.

    It reruns, e.g. hourly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
- The job runs, e.g. nightly.

  ## Steps, e.g. build

  :::{note} <!-- same finding -->
  :class: tip <!-- same finding -->
and lazily, e.g. hourly.

    It reruns, e.g. hourly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
- The job runs, e.g. nightly,
and lazily, e.g. hourly.

  ## Steps, e.g. build

    It reruns, e.g. hourly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
<span>The job</span> runs, e.g. nightly.
- The job runs, e.g. nightly.
  ## Steps, e.g. build

    It reruns, e.g. hourly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
## Setup, e.g. build
2. The job runs, e.g. nightly.
   ## Steps, e.g. build

    It reruns, e.g. hourly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
<!-- same finding -->
2. The job runs, e.g. nightly.
   ## Steps, e.g. build

    It reruns, e.g. hourly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
The job runs, e.g. nightly.
    <div> It runs, e.g. now.
- The job runs, e.g. nightly.

    It reruns, e.g. hourly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
10.  The job runs, e.g. nightly.
    - first, e.g. now

     It reruns, e.g. hourly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
- > <div> <!-- same finding -->
  The job runs, e.g. nightly,
and lazily, e.g. hourly.

    It reruns, e.g. hourly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
- > <div> <!-- same finding -->
  >
  > The job runs, e.g. nightly,
and lazily, e.g. hourly.

    It reruns, e.g. hourly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
- >     <div> <!-- same finding -->
  > The job runs, e.g. nightly,
and lazily, e.g. hourly.

    It reruns, e.g. hourly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
<pre>x</pre> <!-- same finding -->
- The job runs, e.g. nightly.

    It reruns, e.g. hourly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
<pre> <!-- same finding -->
x <!-- same finding -->
</pre> <!-- same finding -->
- The job runs, e.g. nightly.

    It reruns, e.g. hourly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
<script src="x.js"></script> <!-- same finding -->
- The job runs, e.g. nightly.

    It reruns, e.g. hourly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
<textarea> <!-- same finding -->
</style> <!-- same finding -->
- The job runs, e.g. nightly.

    It reruns, e.g. hourly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
<style> <!-- same finding -->
</textarea> <!-- same finding -->
- The job runs, e.g. nightly.

    It reruns, e.g. hourly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
<pre> <!-- same finding -->
<div> <!-- same finding -->
</PRE> <!-- same finding -->
- The job runs, e.g. nightly.

    It reruns, e.g. hourly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
<?xml version="1.0"?> <!-- same finding -->
- The job runs, e.g. nightly.

    It reruns, e.g. hourly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
<!DOCTYPE html> <!-- same finding -->
- The job runs, e.g. nightly.

    It reruns, e.g. hourly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
<![CDATA[x]]> <!-- same finding -->
- The job runs, e.g. nightly.

    It reruns, e.g. hourly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
- <pre> <!-- same finding -->
  x <!-- same finding -->
  </pre> <!-- same finding -->
  The job runs, e.g. nightly,
and lazily, e.g. hourly.

    It reruns, e.g. hourly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
- <pre>x</pre> <!-- same finding -->
  - The job runs, e.g. nightly,
and lazily, e.g. hourly.

    It reruns, e.g. hourly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
The job runs, e.g. nightly,
2. <div> and it runs, e.g. now.
- The job runs, e.g. nightly.

    It reruns, e.g. hourly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
1. The job runs, e.g. nightly.
   -    The job runs, e.g. nightly.

    It reruns, e.g. hourly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
> A quote, e.g. nightly,
    >> and more, e.g. now.
10. Then it stops, e.g. now.

    It reruns, e.g. hourly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
[^1]: The note, e.g. now.
2. Then it stops, e.g. now.

    It reruns, e.g. hourly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
[^1]: The note, e.g. now,
and lazily, e.g. now.
2. Then it stops, e.g. now.

    It reruns, e.g. hourly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
- The job runs, e.g. nightly.
  ## Steps, e.g. now
  [^1]: The note, e.g. now.
  2. Then it stops, e.g. now.

      It reruns, e.g. hourly.
<!-- prettier-ignore-end -->
