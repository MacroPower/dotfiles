**Note:** You must restart the daemon after the upgrade.

The daemon reads its config at startup. <!-- same finding -->

**Note:** The tool prints “done.” Run the migration first.

> [!NOTE]
> Run `task switch` after you edit the file. <!-- same finding -->

> [!WARNING]
> Deleting the volume erases its data. <!-- same finding -->

The daemon writes its log at shutdown. <!-- same finding -->

**Note:** The tool prints ‘done.’ Run the migration first.

The migration reads the lock file. <!-- same finding -->

> [!NOTE]
> Don’t delete the lock file by hand. <!-- same finding -->

The job writes the lock file at startup. <!-- same finding -->

> [!NOTE]
> ‘Remove’ the lock file only after the job stops. <!-- same finding -->

The cleanup removes the temporary files. <!-- same finding -->

**Note:** The tool prints "Done!" always. Run the cleanup after it.

The cleanup writes a log. <!-- same finding -->

**Note:** The cleanup takes a minute. remove the log after it.
