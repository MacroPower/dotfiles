// Package logrotate keeps an append-only log file bounded by size.
//
// A [*Rotator] checks the size of its log before the caller opens the
// log. When the log has reached the size limit, the rotator renames it
// to the same path with ".1" appended. The caller's open then creates
// an empty log. Each rotation moves every rotated file one number up
// and removes each file whose number passes the backup count. The log
// and its rotated files therefore hold little more than the size limit
// times one more than the backup count.
//
// The package serves many short-lived processes that append to one
// log. Each process checks the size one time, before it opens the log.
// The log therefore passes the limit by the records that the processes
// already holding it write. Those processes keep their open file
// through the rename, and their records land in the rotated file.
//
// An exclusive flock on a sibling file, the log path with ".lock"
// appended, serializes rotation. A process that finds the lock held
// skips the rotation, because the holder is rotating the log. A process
// that takes the lock checks the size again, so two processes that saw
// the same full log rotate it one time.
package logrotate
