Pass a flag, such as `--force`, to skip the check.

The file is at /etc/i.e./x.

Run `foo e.g.` in a code span.

Open the folder in VS Code.

Compare tabs vs. spaces.

The loop returns when vs is empty.

>     Run the job, e.g. nightly.

> The quote opens with a paragraph.
>
>     Rerun the job, i.e. hourly.
>
> > The nested quote opens with a paragraph.
> >
> >     Rerun the job, cf. the cron table.

> - The quote holds a list.
>
> The list ends at this paragraph.
>
>     Rerun the job, e.g. hourly.

<!-- prettier-ignore-start -->
> - The quote holds a list.
> # A heading ends it
>     Rerun the job, e.g. hourly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
> - The quote holds a list.
> ```text
> make build
> ```
>     Rerun the job, e.g. hourly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
> > - The nested quote holds a list.
> # A heading ends it
> >     Rerun the job, e.g. hourly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
> > - The nested quote holds a list.
>
> >     Rerun the job, e.g. hourly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
> > - The nested quote holds a list.
> >
> The outer quote goes on.
> >     Rerun the job, e.g. hourly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
>		Run the job, e.g. nightly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
- An item.

    >     Run the job, e.g. nightly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
-	An item after a tab.

      >     Run the job, e.g. nightly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
> The quote opens with a paragraph.
> <!-- An HTML comment ends it. -->
>     Rerun the job, e.g. hourly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
> The quote opens with a paragraph.
> >     Rerun the job, e.g. hourly.
>     Rerun the job, i.e. at noon.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
> - The quote holds a list.
>   >     Rerun the job, e.g. hourly.
> The list ends at this paragraph.
>
>     Rerun the job, i.e. at noon.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
> - The quote holds a list.

>     Rerun the job, e.g. hourly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
- An item.

>     Run the job, e.g. nightly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
- An item.
  > A quote in the item.

    >     Rerun the job, e.g. hourly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
-      Indented code in an item.

    >     Run the job, e.g. nightly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
-   An item.
    > - A list in a quote in the item.

    >     Rerun the job, e.g. hourly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
1. Build the job.
2. Test the job.
    >     Run the job, e.g. nightly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
The steps follow.

2. Build the job.
     >     Run the job, e.g. nightly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
> > - The nested quote holds a list.
> - The outer quote holds a list.
> >     Rerun the job, e.g. hourly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
-   An item.
	-   A nested item after a tab.

        >     Run the job, e.g. nightly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
1. The job runs.
   - It reruns.
    > A quote in the first item.
    >
    >     Run the job, e.g. nightly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
1. The job runs.
   # A heading in the item
    >     Run the job, e.g. nightly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
1. The job runs.
   - It reruns.

    >     Run the job, e.g. nightly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
> The quote opens with a paragraph.
> _ _ _
>     Rerun the job, e.g. hourly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
> - The quote holds a list.
> ***
>     Rerun the job, e.g. hourly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
- > The quote opens in an item.
  - - -
  >     Rerun the job, e.g. hourly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
- > The quote opens in an item.
    # A heading in the item ends it
  >     Rerun the job, e.g. hourly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
<div>The block holds HTML.</div>

- An item.

    >     Run the job, e.g. nightly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
> <div>The quote holds HTML.</div>
- An item.

    >     Run the job, e.g. nightly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
- > 1234567890. The quote opens in an item.
  >
  >     Rerun the job, e.g. hourly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->

-
  The item opens on the line after its marker.
     >     Run the job, e.g. nightly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->

-
  The item opens on the line after its marker.

  It goes on after a blank line.
    >     Run the job, e.g. nightly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
- An item.

  -


  It goes on.
    >     Run the job, e.g. nightly.
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
- The item holds a quote.
  >     ```sh
    ```
    Run the job, e.g. hourly.
    ```
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
>     ```sh
   ```
Run the job, e.g. hourly.
   ```
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
> - The quote holds a list.
>   >     ```sh
>     ```
>     Run the job, e.g. hourly.
>     ```
<!-- prettier-ignore-end -->

<!-- prettier-ignore-start -->
<b>The line</b> opens with HTML.
- The item holds a quote.
  >     make build
    ```
    Run the job, e.g. hourly.
    ```
<!-- prettier-ignore-end -->
