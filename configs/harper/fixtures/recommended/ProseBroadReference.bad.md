The default is nil. This means the pool is empty.

The hook reads the lock first. This ensures that writes never overlap.

The flag defaults to five, which means the pool holds five entries.

The token is short-lived. This allows rotation without a restart.

The hook skips the check. This is because the file is generated.
