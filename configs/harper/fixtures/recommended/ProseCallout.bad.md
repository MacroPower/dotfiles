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

The flag reads one directory. <!-- same finding -->

**Note:** The flag takes a directory, e.g. the scratch one. Run the cleanup after it.

The tool waits for the lock. <!-- same finding -->

**Note:** The tool prints Waiting... You must run the cleanup after it.

The migration needs a backup. <!-- same finding -->

**Note:** 'Run the migration first,' the guide says.

The backup holds the lock file. <!-- same finding -->

> [!NOTE]
> 'Don't delete the lock file by hand.' <!-- same finding -->

The cache holds the last response. <!-- same finding -->

Note: The cache holds the last response.
####### Run the migration first. <!-- same finding -->

The cache holds the last response. <!-- same finding -->

Note: The cache holds the last response. Fix
#1234 first. Run the migration first. <!-- same finding -->

The cache holds the last response. <!-- same finding -->

Note: The cache holds the last response. <!-- same finding -->
####### The cache holds the last hundred reads. <!-- same finding -->

Tip: The cache warms in a minute.
