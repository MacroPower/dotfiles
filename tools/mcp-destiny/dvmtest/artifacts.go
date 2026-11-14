package dvmtest

import (
	"context"
	"io"
	"strings"

	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/dvm"
)

// Artifacts serves the artifact index and the artifact files of the
// service from memory, without a server. It has the Config and Artifact
// methods of [*dvm.Client], so it stands in for the client where code reads
// artifacts through an interface. A test edits the fields between calls.
type Artifacts struct {
	// Files holds the body of each artifact file, keyed by the file name.
	Files map[string]string
	// Index holds the file list of each artifact that the artifact index
	// lists, keyed by the artifact name.
	Index map[string][]string
	// Version is the manifest version that the artifact index names.
	Version string
	// Fetched lists the file of each call to Artifact, in the order of the
	// calls.
	Fetched []string
}

// NewArtifacts returns an [*Artifacts] whose artifact index names version.
// Its Files and Index are empty.
func NewArtifacts(version string) *Artifacts {
	return &Artifacts{
		Files:   map[string]string{},
		Index:   map[string][]string{},
		Version: version,
	}
}

// Config returns a configuration whose artifact index names Version and
// lists the files of Index.
func (a *Artifacts) Config(context.Context) (dvm.Config, error) {
	return dvm.Config{Artifacts: dvm.ArtifactIndex{ManifestVersion: a.Version, Files: a.Index}}, nil
}

// Artifact records file in Fetched and returns the body that Files holds
// for it. It returns [dvm.ErrService] for a file that Files lacks.
func (a *Artifacts) Artifact(_ context.Context, file string) (io.ReadCloser, error) {
	a.Fetched = append(a.Fetched, file)

	body, ok := a.Files[file]
	if !ok {
		return nil, dvm.ErrService
	}

	return io.NopCloser(strings.NewReader(body)), nil
}
