package wireinput

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// PollInterval is how long [Wait] sleeps between two reads of the
	// transcripts. Claude Code flushes its transcript queue every 100
	// milliseconds.
	PollInterval = 100 * time.Millisecond

	// firstTail is how much of the end of a transcript the first read
	// takes. A hook runs right after its tool call, so the entry of the
	// call sits near the end of the file.
	firstTail = 1 << 20

	// tailGrowth multiplies the tail for each read that lacks the
	// entry. A batch of large Write calls can push the entry of the
	// first call more than [firstTail] back.
	tailGrowth = 4

	// maxTail is the largest tail the first read takes.
	maxTail = 16 << 20

	// transcriptExt is the extension of a transcript file.
	transcriptExt = ".jsonl"

	// unread marks a transcript that no read has covered.
	unread = -1
)

var (
	// ErrNotFound reports a tool call that no transcript holds an
	// entry for. Claude Code writes the entries of an assistant message
	// after the message ends, so the entry of a call that ran earlier
	// can still arrive. [Wait] covers that case.
	ErrNotFound = errors.New("tool call not in the transcript")

	// ErrNotRecorded reports a tool call whose transcript entry lacks
	// the input from the API response. Waiting does not change the
	// entry.
	ErrNotRecorded = errors.New("transcript entry lacks the wire input")
)

// Lookup returns the input of the tool call toolUseID as the model
// sent it, before Claude Code rewrote it.
//
// transcriptPath, agentID, and toolUseID come from the hook payload of
// the call. transcriptPath names the transcript of the session. When
// agentID names a subagent, Lookup reads the transcripts of that agent
// instead, because Claude Code writes a subagent's entries only there.
//
// Lookup reads each transcript from its end, because a hook runs right
// after its tool call. It returns [ErrNotFound] when no transcript
// holds the call, and [ErrNotRecorded] when the entry of the call
// lacks the input.
func Lookup(transcriptPath, agentID, toolUseID string) (map[string]any, error) {
	return newReader(transcriptPath, agentID, toolUseID).read()
}

// Wait is [Lookup] for a hook that runs before Claude Code has written
// the entry of its tool call. Claude Code runs a tool as soon as the
// model has sent the call. It writes the transcript entries of the
// message after the model has sent all of it. The entry of a call that
// other calls follow therefore lands after its PostToolUse hook starts.
//
// Wait reads the transcripts every [PollInterval] until one holds the
// call or ctx ends. After the first read it reads only the lines that
// Claude Code appended. It returns [ErrNotFound] when ctx ends first.
func Wait(ctx context.Context, transcriptPath, agentID, toolUseID string) (map[string]any, error) {
	reader := newReader(transcriptPath, agentID, toolUseID)

	ticker := time.NewTicker(PollInterval)
	defer ticker.Stop()

	for {
		input, err := reader.read()
		if !errors.Is(err, ErrNotFound) {
			return input, err
		}

		select {
		case <-ctx.Done():
			return nil, ErrNotFound
		case <-ticker.C:
		}
	}
}

// reader searches the transcripts of one tool call. It remembers how
// far it has read each transcript, so a second read covers only the
// lines that arrived after the first.
type reader struct {
	// offsets maps each transcript to the offset where its unread
	// lines start.
	offsets        map[string]int64
	transcriptPath string
	agentID        string
	toolUseID      string
}

func newReader(transcriptPath, agentID, toolUseID string) *reader {
	return &reader{
		offsets:        map[string]int64{},
		transcriptPath: transcriptPath,
		agentID:        agentID,
		toolUseID:      toolUseID,
	}
}

// read searches the unread lines of every transcript for the call.
func (r *reader) read() (map[string]any, error) {
	if r.transcriptPath == "" || r.toolUseID == "" {
		return nil, ErrNotFound
	}

	paths, err := r.transcripts()
	if err != nil {
		return nil, err
	}

	for _, path := range paths {
		offset, ok := r.offsets[path]
		if !ok {
			offset = unread
		}

		input, next, err := readTranscript(path, r.toolUseID, offset)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}

		r.offsets[path] = next

		if errors.Is(err, ErrNotFound) {
			continue
		}

		return input, err
	}

	return nil, ErrNotFound
}

// transcripts returns the transcript files that can hold the call.
// Claude Code writes the entries of a subagent to files under the
// directory that carries the session transcript's name without its
// extension, in subagents or in a directory below it. The files of a
// subagent appear with its first entry, so each read lists them again.
// A call without an agent returns the session transcript, and so does
// a payload whose transcript path already names the agent's file. An
// agentID that is not a plain file name returns nothing.
func (r *reader) transcripts() ([]string, error) {
	if r.agentID == "" {
		return []string{r.transcriptPath}, nil
	}

	if r.agentID != filepath.Base(r.agentID) {
		return nil, nil
	}

	name := "agent-" + r.agentID + transcriptExt
	if filepath.Base(r.transcriptPath) == name {
		return []string{r.transcriptPath}, nil
	}

	root := filepath.Join(strings.TrimSuffix(r.transcriptPath, transcriptExt), "subagents")

	var paths []string

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			// A missing or unreadable directory holds no transcript.
			return fs.SkipDir
		}

		if !entry.IsDir() && entry.Name() == name {
			paths = append(paths, path)
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking %s: %w", root, err)
	}

	return paths, nil
}

// readTranscript searches the lines of one transcript from offset on
// for the call. It returns the offset where the next read starts, which
// is the end of the last whole line. An [unread] offset reads a tail of
// the file and grows the tail until it holds the call, reaches the
// start of the file, or reaches [maxTail].
func readTranscript(path, toolUseID string, offset int64) (map[string]any, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, offset, fmt.Errorf("opening transcript: %w", err)
	}

	input, next, err := searchTranscript(file, toolUseID, offset)

	closeErr := file.Close()
	if closeErr != nil && err == nil {
		return nil, offset, fmt.Errorf("closing transcript: %w", closeErr)
	}

	return input, next, err
}

// searchTranscript is [readTranscript] on the open transcript.
func searchTranscript(file *os.File, toolUseID string, offset int64) (map[string]any, int64, error) {
	info, err := file.Stat()
	if err != nil {
		return nil, offset, fmt.Errorf("reading transcript size: %w", err)
	}

	size := info.Size()

	// A transcript that Claude Code rewrote can be shorter than the
	// part that an earlier read covered.
	if offset > size {
		offset = unread
	}

	start := offset
	tail := int64(firstTail)

	for {
		if offset == unread {
			start = max(size-tail, 0)
		}

		buf := make([]byte, size-start)

		_, err := file.ReadAt(buf, start)
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, offset, fmt.Errorf("reading transcript: %w", err)
		}

		// A tail starts inside a line, and Claude Code can still be
		// writing the last line. Search the whole lines between.
		from := 0
		if offset == unread && start > 0 {
			from = bytes.IndexByte(buf, '\n') + 1
		}

		to := bytes.LastIndexByte(buf, '\n') + 1

		input, err := lookupLines(buf[from:to], toolUseID)
		if !errors.Is(err, ErrNotFound) {
			return input, start + int64(to), err
		}

		if offset != unread || start == 0 || tail >= maxTail {
			return nil, start + int64(to), ErrNotFound
		}

		tail *= tailGrowth
	}
}

// lookupLines searches transcript lines for the entry of the call,
// last line first, and returns the input of the call from
// wireToolInputs. It returns [ErrNotRecorded] for an assistant entry
// that holds the call without that input.
func lookupLines(buf []byte, toolUseID string) (map[string]any, error) {
	id := []byte(toolUseID)

	for len(buf) > 0 {
		cut := bytes.LastIndexByte(buf, '\n')
		line := buf[cut+1:]
		buf = buf[:max(cut, 0)]

		if !bytes.Contains(line, id) {
			continue
		}

		input, err := lookupLine(line, toolUseID)
		if errors.Is(err, ErrNotFound) {
			continue
		}

		return input, err
	}

	return nil, ErrNotFound
}

// lookupLine reads the input of the call from one transcript line. A
// line that is not the assistant entry of the call returns
// [ErrNotFound].
func lookupLine(line []byte, toolUseID string) (map[string]any, error) {
	var entry struct {
		WireToolInputs map[string]json.RawMessage `json:"wireToolInputs"`
		Type           string                     `json:"type"`
		Message        json.RawMessage            `json:"message"`
	}

	err := json.Unmarshal(line, &entry)
	if err != nil || entry.Type != "assistant" {
		return nil, ErrNotFound
	}

	raw, ok := entry.WireToolInputs[toolUseID]
	if ok {
		var input map[string]any

		err = json.Unmarshal(raw, &input)
		if err != nil {
			return nil, ErrNotRecorded
		}

		return input, nil
	}

	var message struct {
		Content []struct {
			Type string `json:"type"`
			ID   string `json:"id"`
		} `json:"content"`
	}

	err = json.Unmarshal(entry.Message, &message)
	if err != nil {
		return nil, ErrNotFound
	}

	for _, block := range message.Content {
		if block.Type == "tool_use" && block.ID == toolUseID {
			return nil, ErrNotRecorded
		}
	}

	return nil, ErrNotFound
}
