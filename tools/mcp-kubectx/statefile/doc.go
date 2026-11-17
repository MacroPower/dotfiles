// Package statefile writes small state files for mcp-kubectx. Its
// writes keep these guarantees:
//
//   - Atomicity: each write goes through a tmp file and a rename. So
//     uncoordinated concurrent readers never observe a torn or
//     zero-byte file.
//   - Mode: [WriteSecure] gives each file mode 0600, which matches the
//     single-user socket trust boundary. [WriteAtomic] gives the file
//     the mode that the caller passes, and the umask does not change
//     it.
//   - Pointer files: the package replaces a symlink with a rename, so
//     a reader never finds the symlink missing.
//   - Symlinks: [WriteAtomic] and [WriteSecure] never write through a
//     symlink at the path or at a tmp name. Each creates a new tmp
//     file with O_EXCL under a random name and renames it to the path.
//     So a process that can change the entries of the directory cannot
//     send the data to another file. A Lima guest is such a process
//     for each directory that workmux mounts writable into the guest.
//     Both writes still follow a symlink in a directory component of
//     the path. So a path in such a mount must name a direct child of
//     the mount root, because the guest cannot replace the mount root.
//
// [SymlinkAtomic] writes no file data. It creates its tmp symlink at
// the fixed name <path>.tmp. So a process that can change the entries
// of the directory can make [SymlinkAtomic] fail, for example with a
// non-empty directory at that name.
package statefile
