Close releases the file handle. This is deliberate.
Close releases the file handle. This is intentional.
The cache is cold on first run by design.
The function is by design idempotent.
Len counts expired entries too. This is intentional.
The pin is unused by design; leave it floating.
The bus is idle-high by design, so a stuck-low line reads as a fault.
