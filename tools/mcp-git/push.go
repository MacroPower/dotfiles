package main

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var (
	// ErrPush wraps the underlying git push failure.
	ErrPush = errors.New("git push failed")

	// ErrNoBranch is returned when a push names no ref and HEAD names
	// no branch, as a detached HEAD does.
	ErrNoBranch = errors.New("HEAD names no branch")

	// ErrUnknownRef is returned when a push names a ref that is not a
	// local branch or a tag.
	ErrUnknownRef = errors.New("ref names no local branch and no tag")

	// ErrAmbiguousRef is returned when a push names a ref that is both
	// a local branch and a tag.
	ErrAmbiguousRef = errors.New("ref names both a branch and a tag")
)

// PushInput is the JSON input schema for the git_push tool.
type PushInput struct {
	Repo           string `json:"repo"                      jsonschema:"Path to an existing local git repository"`
	Remote         string `json:"remote,omitzero"           jsonschema:"Remote name to push to (default origin)"`
	Ref            string `json:"ref,omitzero"              jsonschema:"Branch or tag name to push; omit to push the current branch"`
	SetUpstream    bool   `json:"set_upstream,omitzero"     jsonschema:"Record the remote branch as the upstream of the pushed branch"`
	ForceWithLease bool   `json:"force_with_lease,omitzero" jsonschema:"Overwrite the remote branch only if it still matches the local remote-tracking ref"`
	Tags           bool   `json:"tags,omitzero"             jsonschema:"Also push all tags. With no ref, push only the tags"`

	// TimeoutSeconds overrides the server default for this call.
	// Zero means use the default; per-call disable is not supported.
	TimeoutSeconds int `json:"timeout_seconds,omitzero" jsonschema:"Override the default per-operation timeout in seconds; 0 means use the server default"`
}

func (h *handler) handlePush(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	input PushInput,
) (*mcp.CallToolResult, any, error) {
	remote := remoteOrDefault(input.Remote)

	inputErr := checkRemoteInputs(input.Repo, remote, input.Ref)
	if inputErr != nil {
		return toolError(inputErr), nil, nil
	}

	out, err := h.runRemote(ctx, remoteOp{
		repo:    input.Repo,
		remote:  remote,
		push:    true,
		timeout: input.TimeoutSeconds,
		buildArgs: func(ctx context.Context, repo string) ([]string, error) {
			return pushArgs(ctx, repo, remote, input)
		},
		// A pre-push hook is repository policy, and the push skips
		// every pre-push hook. The note tells the caller to run the
		// hook itself, in the sandbox.
		skippedHook: "pre-push",
	})
	if err != nil {
		return toolError(fmt.Errorf("%w: %w", ErrPush, err)), nil, nil
	}

	return remoteResult(
		fmt.Sprintf("Pushed to %s from %s", remote, input.Repo),
		out,
	), nil, nil
}

// pushArgs returns the argument list for the git push that input
// describes in the repository at the absolute path repo. The push
// updates the branch or the tag that input names, or the current
// branch when input names no ref. An input that names no ref and sets
// Tags gets no refspec, and git push --tags then pushes the tags
// alone.
//
// The refspec names the same full ref on both sides. git maps a plain
// name such as "feature" through remote.<name>.push, push.default,
// and the upstream of the branch. The config of the repository sets
// each of them, so a plain name can update another ref of the remote,
// such as the default branch. git takes a refspec with two sides as
// it is. The refspec never starts with '+' and never has an empty
// source, so it forces no update and deletes no ref.
//
// git applies push.autoSetupRemote only to a push that names no
// refspec. pushArgs therefore applies the key itself, as
// [autoSetsUpstream] describes.
func pushArgs(ctx context.Context, repo, remote string, input PushInput) ([]string, error) {
	if input.Ref == "" && input.Tags {
		return buildPushArgs(remote, input, ""), nil
	}

	name, err := pushRefName(ctx, repo, input.Ref)
	if err != nil {
		return nil, err
	}

	if input.Ref == "" && !input.SetUpstream {
		input.SetUpstream, err = autoSetsUpstream(ctx, repo, name)
		if err != nil {
			return nil, err
		}
	}

	return buildPushArgs(remote, input, name+":"+name), nil
}

// buildPushArgs converts a [PushInput] into the argument list for
// git push, targeting the already-resolved remote name. refspec is
// the refspec of [pushArgs], or "" for a push that names none. The
// list never holds a bare --force.
//
// The list always holds --no-verify, which makes git skip the
// pre-push event. [handler.runRemote] disables the hooks directory.
// --no-verify also skips a pre-push hook that git config defines with
// hook.<name>.command. Git does not pass --no-verify on to the push
// that it starts in a submodule, so [pushOptions] keeps git from
// starting such a push.
//
// The list also holds the options that keep the push from running a
// command that the git config names. --no-signed keeps git from
// signing the push with the program of gpg.program, which
// push.gpgSign asks for. --receive-pack names the program that serves
// the push. Its value is the default of git, and it wins over
// remote.<name>.receivepack. git runs that program on the host of an
// ssh remote.
//
// --no-follow-tags wins over push.followTags, which makes git push
// each annotated tag that points into the pushed history.
func buildPushArgs(remote string, input PushInput, refspec string) []string {
	args := []string{"push", "--no-verify", "--no-signed", "--no-follow-tags", "--receive-pack=git-receive-pack"}

	if input.SetUpstream {
		args = append(args, "--set-upstream")
	}

	if input.ForceWithLease {
		args = append(args, "--force-with-lease")
	}

	if input.Tags {
		args = append(args, "--tags")
	}

	args = append(args, "--", remote)

	if refspec != "" {
		args = append(args, refspec)
	}

	return args
}

// autoSetsUpstream reports whether a push of the current branch
// records the remote branch as the upstream of the branch, without
// set_upstream. ref is the full name of the branch. git records the
// upstream when push.autoSetupRemote is true and the branch has no
// upstream. The upstream is a setting of the local repository, so the
// key changes nothing on the remote.
func autoSetsUpstream(ctx context.Context, repo, ref string) (bool, error) {
	auto, err := configValue(ctx, repo, "push.autoSetupRemote", "--type=bool")
	if err != nil || auto != "true" {
		return false, err
	}

	branch := strings.TrimPrefix(ref, "refs/heads/")

	// A branch has an upstream when the config names both the remote
	// and the branch of the remote.
	for _, key := range []string{"remote", "merge"} {
		value, err := configValue(ctx, repo, "branch."+branch+"."+key)
		if err != nil {
			return false, err
		}

		if value == "" {
			return true, nil
		}
	}

	return false, nil
}

// pushRefName returns the full name of the local ref that a push of
// ref updates on the remote. An empty ref names the current branch.
// Every other ref is the name of a local branch or a tag, such as
// "feature" or "v1.0.0", or a full name under refs/heads/ or
// refs/tags/.
func pushRefName(ctx context.Context, repo, ref string) (string, error) {
	if ref == "" {
		return currentBranchRef(ctx, repo)
	}

	names := []string{"refs/heads/" + ref, "refs/tags/" + ref}
	if strings.HasPrefix(ref, "refs/heads/") || strings.HasPrefix(ref, "refs/tags/") {
		names = []string{ref}
	}

	out, err := gitValue(ctx, slices.Concat(
		[]string{"-C", repo, "for-each-ref", "--format=%(refname)"}, names,
	)...)
	if err != nil {
		if errors.Is(err, ErrTimeout) {
			return "", err
		}

		return "", fmt.Errorf("listing refs: %w", err)
	}

	var found []string

	// git also lists the refs under a name, such as refs/heads/feat/x
	// for refs/heads/feat.
	for name := range strings.SplitSeq(out, "\n") {
		if slices.Contains(names, name) {
			found = append(found, name)
		}
	}

	switch len(found) {
	case 0:
		return "", fmt.Errorf("%w: %q", ErrUnknownRef, ref)
	case 1:
		return found[0], nil
	default:
		return "", fmt.Errorf("%w: %q. Name %s", ErrAmbiguousRef, ref, strings.Join(found, " or "))
	}
}

// currentBranchRef returns the full name of the branch that HEAD
// names in the repository at repo, such as "refs/heads/main".
func currentBranchRef(ctx context.Context, repo string) (string, error) {
	const advice = "Name the branch or the tag to push in ref"

	out, err := gitValue(ctx, "-C", repo, "symbolic-ref", "--quiet", "HEAD")
	if err != nil {
		// With --quiet, git exits 1 when HEAD is detached.
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return "", fmt.Errorf("%w. %s", ErrNoBranch, advice)
		}

		if errors.Is(err, ErrTimeout) {
			return "", err
		}

		return "", fmt.Errorf("resolving HEAD: %w", err)
	}

	if !strings.HasPrefix(out, "refs/heads/") {
		return "", fmt.Errorf("%w. %s", ErrNoBranch, advice)
	}

	return out, nil
}
