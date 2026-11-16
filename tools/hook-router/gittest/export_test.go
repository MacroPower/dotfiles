package gittest

var (
	// InitRepository creates a repository with one staged file, and it
	// returns the path of the repository.
	InitRepository = initRepository

	// ReadTree returns the mode of each file and each directory under a
	// root, by path, with the SHA-256 hash of each regular file.
	ReadTree = readTree
)
