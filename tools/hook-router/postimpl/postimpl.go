package postimpl

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Skill describes one post-implementation slash command Claude may
// invoke when a plan's Stop gate fires. The shape mirrors the JSON
// emitted by the Nix-side `postImplSkills` list: field order and tag
// casing must match or [builtins.toJSON] output will silently unmarshal
// to zero values.
type Skill struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}

// Catalog bundles a list of [Skill] entries (used for block-message
// rendering) with the derived label set (used for O(1) validation of
// AskUserQuestion option labels). Construct with [New] so the two stay
// in sync.
type Catalog struct {
	labels map[string]bool
	skills []Skill
}

// New builds a [*Catalog] from the given skills, folding each skill's
// label into the validation set. Duplicates across entries are not
// deduped: the Nix list is the source of truth and is expected to stay
// clean.
func New(skills []Skill) *Catalog {
	labels := make(map[string]bool, len(skills))

	for _, s := range skills {
		labels[s.Label] = true
	}

	return &Catalog{skills: skills, labels: labels}
}

// HasLabel reports whether label matches a slash-command label in the
// catalog.
func (c *Catalog) HasLabel(label string) bool { return c.labels[label] }

// Empty reports whether the catalog has no skills.
func (c *Catalog) Empty() bool { return len(c.skills) == 0 }

// BuildAskReason returns the unified Stop block-message used while a
// session is mid-implementation. The message guides Claude through
// three cases, ordered so the clarifying-question path wins when it
// applies (the model tends to default to whichever branch is most
// concrete):
//
//   - "If you have a question for the user": call AskUserQuestion with
//     that question, not with the post-impl labels. This branch goes
//     first because the failure mode it guards against is Claude
//     selecting a post-impl label when it actually meant to ask a
//     clarifying question.
//   - "If you are not done": keep working.
//   - "Only when you have completed the implementation": call
//     AskUserQuestion with the catalog's slash-command labels and then
//     invoke each chosen option as a slash command.
//
// Bullets render in catalog order (Nix list order, preserved through
// [builtins.toJSON]). When the catalog is empty the bullet section is
// suppressed and the "completed" branch falls back to a single sentence
// — production should never hit this path (the caller logs a warning at
// startup) but the invariant that callers can render without a
// nil-guard holds.
//
// Wording note: the message describes what Claude should do, not what
// the gate is checking. Disclosing the precise unlock condition (e.g.
// "Stop unlocks when a post-impl AUQ is answered against the current
// git state") tends to make the model optimize for the unlock rather
// than for the work the unlock is meant to gate.
func (c *Catalog) BuildAskReason(planPath, baseSHA string) string {
	var b strings.Builder

	fmt.Fprintf(&b, "You are implementing the plan at %s (baseline: %s).\n\n",
		planPath, baseSHA)

	b.WriteString("If you have a question for the user (including one you wrote out" +
		" but did not deliver via AskUserQuestion before ending your turn) call" +
		" AskUserQuestion now with that question. The post-implementation options" +
		" below are not answers to clarifying questions; their labels are slash" +
		" commands, not free-text responses.\n")

	b.WriteString("If you are not done, keep working.\n")

	if len(c.skills) > 0 {
		b.WriteString("Only when you have completed the implementation AND have no" +
			" outstanding question for the user, call AskUserQuestion with the" +
			" post-implementation review options below. Each option's `label` MUST" +
			" be exactly one of:\n")

		for _, s := range c.skills {
			fmt.Fprintf(&b, "  - %s: %s\n", s.Label, s.Description)
		}

		b.WriteString("Each option's `label` is itself a slash-command invocation." +
			" After the user answers, run each chosen option's label as the" +
			" corresponding slash command. Order the commands intelligently: edits" +
			" first, then reviews, then any finalizers.")
	} else {
		b.WriteString("Only when you have completed the implementation AND have no" +
			" outstanding question for the user, call AskUserQuestion with the" +
			" post-implementation review options provided by your environment.")
	}

	return b.String()
}

// Parse decodes the JSON payload passed via --post-impl-skills into a
// [*Catalog]. An empty input yields an empty catalog (valid for tests
// and early-startup paths); malformed JSON returns an error so wrapper
// misconfiguration is loud.
func Parse(s string) (*Catalog, error) {
	if s == "" {
		return New(nil), nil
	}

	var skills []Skill

	err := json.Unmarshal([]byte(s), &skills)
	if err != nil {
		return nil, fmt.Errorf("decoding post-impl skills JSON: %w", err)
	}

	return New(skills), nil
}
