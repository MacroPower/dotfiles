package autotag

import (
	"cmp"
	"errors"
	"slices"
	"strings"

	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/bungie"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/definitions"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/dvm"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/inventory"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/rolls"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/search"
)

// markDigits is the width of an autotag mark. The service compares marks
// as text, so every mark has this many digits.
const markDigits = 20

// markOf returns instanceID in the form of an autotag mark, padded with
// zeros on the left to [markDigits] digits. The web client and the service
// compare marks as text, and the padding lets that comparison order ids of
// different lengths.
func markOf(instanceID string) string {
	return strings.Repeat("0", max(markDigits-len(instanceID), 0)) + instanceID
}

// NewestMark returns the highest [markOf] among the items of s that the
// web client treats as acquired drops, which are the items of
// [search.OwnedItems]. The web client treats every such item with a higher
// mark as newly acquired. An inventory without such an item gives the mark
// of zeros.
func NewestMark(s *inventory.Snapshot) string {
	newest := markOf("")

	for _, item := range search.OwnedItems(s) {
		mark := markOf(item.InstanceID)
		if mark > newest {
			newest = mark
		}
	}

	return newest
}

// NewItems returns the items of s in [search.OwnedItems] whose [markOf] is
// higher than mark, which the next claim covers. The first list holds the
// items outside the vault, which the claim offers to the autotag rules.
// The second list holds the items in the vault. Both lists follow the
// order of [inventory.CompareIDs].
func NewItems(s *inventory.Snapshot, mark string) ([]*inventory.Item, []*inventory.Item) {
	var held, vault []*inventory.Item

	for _, item := range search.OwnedItems(s) {
		switch {
		case markOf(item.InstanceID) <= mark:
		case item.Owner == inventory.OwnerVault:
			vault = append(vault, item)
		default:
			held = append(held, item)
		}
	}

	byID := func(a, b *inventory.Item) int { return inventory.CompareIDs(a.InstanceID, b.InstanceID) }

	slices.SortStableFunc(held, byID)
	slices.SortStableFunc(vault, byID)

	return held, vault
}

// LiveRules drops the removed rules and returns the rest in evaluation
// order.
func LiveRules(rules []dvm.AutotagRule) []dvm.AutotagRule {
	live := slices.DeleteFunc(slices.Clone(rules), func(rule dvm.AutotagRule) bool { return rule.Removed })

	slices.SortStableFunc(live, func(a, b dvm.AutotagRule) int { return cmp.Compare(a.Position, b.Position) })

	return live
}

// RuleClass tells what the server knows about one autotag rule before it
// reads any item.
type RuleClass uint8

// The classes of a [RuleEval].
const (
	// RuleEvaluated marks a rule whose query the server evaluates.
	RuleEvaluated RuleClass = iota
	// RuleSkipped marks a rule that the web client skips, so the rule
	// takes no item.
	RuleSkipped
	// RuleBlocked marks a rule that the web client runs, or might run,
	// and that the server cannot evaluate. The server then cannot tell
	// which items the rule takes, and so which items reach the later
	// rules.
	RuleBlocked
)

// RuleEval is the class of one live autotag rule before any item. A
// skipped rule and a blocked rule carry the reason of the class, and an
// evaluated rule carries the test of its query. The class ignores whether
// the rule is enabled.
type RuleEval struct {
	// Test is the test of the compiled query of an evaluated rule. It is
	// nil for the other classes.
	Test func(*inventory.Item) search.Outcome
	// Reason is the skip reason of a skipped rule or the block reason of
	// a blocked rule. It is empty for an evaluated rule.
	Reason string
	Rule   dvm.AutotagRule
	Class  RuleClass
}

// EvalRules returns the [RuleEval] of each rule of rules, in the same
// order. It classifies a rule by the first problem of its query. The
// problems come in this order: a parse error, a value that the web client
// rejects, a term that the server does not evaluate, and a game mode
// without ratings. A parse error that the web client runs blocks the rule,
// and any other parse error skips it, as an invalid value does. A rule
// without a problem gets the evaluated class.
//
// The tests of the evaluated rules share one search context over every
// built item of s, the vault included, as the web client passes every item
// to its filters. Sources returns the [rolls.RatingSource] of each game mode.
// EvalRules calls sources one time, and only when a rule of rules without
// an earlier problem reads a game mode.
func EvalRules(
	s *inventory.Snapshot,
	rules []dvm.AutotagRule,
	sources func() map[string]rolls.RatingSource,
) []RuleEval {
	evals := make([]RuleEval, len(rules))
	queries := make([]search.CompiledQuery, len(rules))
	rated := false

	for i, rule := range rules {
		evals[i].Rule = rule

		root, err := search.ParseQuery(rule.Query)
		if err != nil {
			var queryErr *search.QueryError
			if errors.As(err, &queryErr) && queryErr.WebClientRuns() {
				evals[i].Class = RuleBlocked
				evals[i].Reason = "the web client runs this rule, and the server cannot evaluate it: " + err.Error()
			} else {
				evals[i].Class = RuleSkipped
				evals[i].Reason = err.Error()
			}

			continue
		}

		query := search.CompileQuery(root)

		switch {
		case len(query.Invalid) > 0:
			evals[i].Class = RuleSkipped
			evals[i].Reason = "the web client rejects " + strings.Join(query.Invalid, ", ")

		case len(query.Unsupported) > 0:
			evals[i].Class = RuleBlocked
			evals[i].Reason = "the server does not evaluate " + strings.Join(query.Unsupported, ", ")

		default:
			queries[i] = query
			rated = rated || len(query.Modes) > 0
		}
	}

	var ratings map[string]rolls.RatingSource
	if rated {
		ratings = sources()
	}

	current, _ := s.LastPlayed()
	sctx := search.NewContext(s, current, ratings)

	for i := range evals {
		if evals[i].Class != RuleEvaluated {
			continue
		}

		reason := queries[i].UnratedReason(ratings)
		if reason != "" {
			evals[i].Class = RuleBlocked
			evals[i].Reason = reason

			continue
		}

		evals[i].Test = queries[i].Test(sctx)
	}

	return evals
}

// NewMatches is what [FirstMatch] gives the new items. Each list follows
// the order of the items that FirstMatch reads.
type NewMatches struct {
	// Taken holds the items that each rule takes, at the index of its
	// [RuleEval].
	Taken [][]*inventory.Item
	// Undecided holds the items that an enabled rule leaves undecided
	// before any rule matches them.
	Undecided []UndecidedAt
	// Reaching holds the items that reach the first enabled rule that
	// blocks.
	Reaching []*inventory.Item
	// Untaken holds the items that no rule takes. It is empty when an
	// enabled rule blocks.
	Untaken []*inventory.Item
	// Blocked is the index of the [RuleEval] of the first enabled rule
	// that blocks, or -1 when no enabled rule blocks.
	Blocked int
	// Offered is the number of items that FirstMatch reads.
	Offered int
}

// UndecidedAt is a new item that an enabled rule leaves undecided, with
// the index of the [RuleEval] of the rule and the reasons of the outcome.
type UndecidedAt struct {
	Item    *inventory.Item
	Reasons []string
	Rule    int
}

// FirstMatch gives each item of items to the first enabled rule of evals,
// in order, whose query matches the item, as the web client does. For
// each item, it walks the enabled rules. It passes a skipped rule, and it
// stops at a blocked rule, because that rule might take the item. It
// stops at a rule that leaves the item undecided, because that rule might
// take the item too.
func FirstMatch(evals []RuleEval, items []*inventory.Item) NewMatches {
	m := NewMatches{
		Taken:   make([][]*inventory.Item, len(evals)),
		Blocked: slices.IndexFunc(evals, func(e RuleEval) bool { return e.Rule.Enabled && e.Class == RuleBlocked }),
		Offered: len(items),
	}

	walked := evals
	if m.Blocked >= 0 {
		walked = evals[:m.Blocked]
	}

items:
	for _, item := range items {
		for i, e := range walked {
			if !e.Rule.Enabled || e.Class != RuleEvaluated {
				continue
			}

			got := e.Test(item)

			switch got.State {
			case search.Match:
				m.Taken[i] = append(m.Taken[i], item)

				continue items

			case search.Undecided:
				m.Undecided = append(m.Undecided, UndecidedAt{Item: item, Reasons: got.Reasons, Rule: i})

				continue items

			case search.NoMatch:
			}
		}

		if m.Blocked >= 0 {
			m.Reaching = append(m.Reaching, item)
		} else {
			m.Untaken = append(m.Untaken, item)
		}
	}

	return m
}

// RuleCount is what [OwnedCounts] gives one live rule.
type RuleCount struct {
	// Reasons holds each distinct reason of the undecided items, in the
	// order of the items.
	Reasons []string
	Eval    RuleEval
	// Matches is the number of items that the rule matches among the
	// items that no earlier enabled rule took.
	Matches int
	// Undecided is the number of those items whose outcome the server
	// cannot decide.
	Undecided int
	// Unknown marks an evaluated or blocked rule after an enabled rule
	// that blocks or that leaves an item undecided. The server then
	// cannot tell which items reach the rule, so it counts nothing for
	// the rule.
	Unknown bool
}

// OwnedCounts counts the items of items that each rule of evals matches,
// as the rule editor of the web client counts them. It walks the rules in
// order and counts each evaluated rule over the items that no earlier
// enabled rule took. An enabled rule then takes the items that it
// matches, and a disabled rule takes nothing. A skipped rule counts
// nothing and takes nothing, wherever it stands.
//
// The server cannot tell which items an enabled rule takes when the rule
// blocks or leaves an item undecided. OwnedCounts therefore marks each
// later rule of the evaluated or the blocked class as unknown.
func OwnedCounts(evals []RuleEval, items []*inventory.Item) []RuleCount {
	counts := make([]RuleCount, len(evals))
	taken := map[string]bool{}
	unknown := false

	for i, e := range evals {
		c := &counts[i]
		c.Eval = e

		switch {
		case e.Class == RuleSkipped:
			continue

		case unknown:
			c.Unknown = true

			continue

		case e.Class == RuleBlocked:
			unknown = e.Rule.Enabled

			continue
		}

		var matched []string

		for _, item := range items {
			if taken[item.InstanceID] {
				continue
			}

			got := e.Test(item)

			switch got.State {
			case search.Match:
				matched = append(matched, item.InstanceID)
			case search.Undecided:
				c.Undecided++

				for _, reason := range got.Reasons {
					c.Reasons = appendNew(c.Reasons, reason)
				}

			case search.NoMatch:
			}
		}

		c.Matches = len(matched)

		if e.Rule.Enabled {
			for _, id := range matched {
				taken[id] = true
			}

			unknown = c.Undecided > 0
		}
	}

	return counts
}

// appendNew appends term to terms when terms does not hold it yet.
func appendNew(terms []string, term string) []string {
	if slices.Contains(terms, term) {
		return terms
	}

	return append(terms, term)
}

// RuleTag is one new item that an enabled rule with a tag takes.
type RuleTag struct {
	Item *inventory.Item
	Rule dvm.AutotagRule
}

// RuleTags returns each new item of m that a rule with a tag takes, in
// rule order and then in the order of the items. A rule without a tag
// takes its items and tags none of them.
func RuleTags(evals []RuleEval, m NewMatches) []RuleTag {
	var tags []RuleTag

	for i, e := range evals {
		if e.Rule.TagID == "" {
			continue
		}

		for _, item := range m.Taken[i] {
			tags = append(tags, RuleTag{Item: item, Rule: e.Rule})
		}
	}

	return tags
}

// VaultItem is one new item that an enabled rule with the vault flag
// takes. Rule is the rule that takes the item. Reason keeps the item in
// place, as [VaultBlock] gives it, and is empty for an item that
// run_autotag_rules moves to the vault. Err is the error of a move that
// failed. It is nil after a move that succeeded and for an item with a
// reason.
type VaultItem struct {
	Item   *inventory.Item
	Err    error
	Reason string
	Rule   dvm.AutotagRule
}

// VaultItems returns the new items of m that the rules of evals with the
// vault flag take, in the order of [inventory.CompareIDs], each with its
// rule and the reason of [VaultBlock]. Defs holds the definition of each
// item.
func VaultItems(evals []RuleEval, m NewMatches, defs *definitions.Defs) []VaultItem {
	var vaulted []VaultItem

	for i, e := range evals {
		if !e.Rule.Vault {
			continue
		}

		for _, item := range m.Taken[i] {
			vaulted = append(vaulted, VaultItem{
				Item:   item,
				Reason: VaultBlock(item, defs.Items[item.Hash]),
				Rule:   e.Rule,
			})
		}
	}

	slices.SortStableFunc(vaulted, func(a, b VaultItem) int {
		return inventory.CompareIDs(a.Item.InstanceID, b.Item.InstanceID)
	})

	return vaulted
}

// VaultBlock returns the reason that keeps item in place when an autotag
// rule with the vault flag takes it. It returns an empty string for an
// item that run_autotag_rules moves to the vault. Def is the definition of
// the item.
//
// The web client leaves the item of such a rule in place when a character
// has the item equipped, or when Bungie gives it the transfer status
// NotTransferrable alone. It also leaves an item in place when the
// definition bucket of the item is the bucket of the distorted engrams or
// a bucket of the Postmaster category of DIM. The web client moves every
// other item, and it pulls an item from the Postmaster first, as
// [inventory.Snapshot.Move] does.
//
// [inventory.Item.Transferable] also reads the definition and every
// transfer status with the NotTransferrable bit, and
// [inventory.Snapshot.Move] refuses an item that it does not mark.
// VaultBlock leaves such an item in place with a reason of its own, which
// says that the web client tries the transfer.
func VaultBlock(item *inventory.Item, def definitions.ItemDef) string {
	switch {
	case item.Equipped:
		return "it is equipped"

	case item.TransferStatus == bungie.TransferNotTransferable:
		return "Bungie reports it as not transferable"

	case def.Bucket == bungie.BucketForbidden,
		def.Bucket == bungie.BucketPostmaster,
		slices.Contains(search.PostmasterBuckets, def.Bucket):
		return "the web client never moves an item of its bucket"

	case !item.Transferable:
		return "the server treats it as not transferable, but the web client tries the transfer"

	default:
		return ""
	}
}
