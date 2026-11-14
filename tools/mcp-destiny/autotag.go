package main

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

// newestMark returns the highest [markOf] among the items of s that the
// web client treats as acquired drops, which are the items of
// [search.OwnedItems]. The web client treats every such item with a higher
// mark as newly acquired. An inventory without such an item gives the mark
// of zeros.
func newestMark(s *inventory.Snapshot) string {
	newest := markOf("")

	for _, item := range search.OwnedItems(s) {
		mark := markOf(item.InstanceID)
		if mark > newest {
			newest = mark
		}
	}

	return newest
}

// newItems returns the items of s in [search.OwnedItems] whose [markOf] is
// higher than mark, which the next claim covers. The first list holds the
// items outside the vault, which the claim offers to the autotag rules.
// The second list holds the items in the vault. Both lists follow the
// order of [inventory.CompareIDs].
func newItems(s *inventory.Snapshot, mark string) ([]*inventory.Item, []*inventory.Item) {
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

// liveRules drops the removed rules and returns the rest in evaluation
// order.
func liveRules(rules []dvm.AutotagRule) []dvm.AutotagRule {
	live := slices.DeleteFunc(slices.Clone(rules), func(rule dvm.AutotagRule) bool { return rule.Removed })

	slices.SortStableFunc(live, func(a, b dvm.AutotagRule) int { return cmp.Compare(a.Position, b.Position) })

	return live
}

// ruleClass tells what the server knows about one autotag rule before it
// reads any item.
type ruleClass uint8

// The classes of a [ruleEval].
const (
	// ruleEvaluated marks a rule whose query the server evaluates.
	ruleEvaluated ruleClass = iota
	// ruleSkipped marks a rule that the web client skips, so the rule
	// takes no item.
	ruleSkipped
	// ruleBlocked marks a rule that the web client runs, or might run,
	// and that the server cannot evaluate. The server then cannot tell
	// which items the rule takes, and so which items reach the later
	// rules.
	ruleBlocked
)

// ruleEval is the class of one live autotag rule before any item. A
// skipped rule and a blocked rule carry the reason of the class, and an
// evaluated rule carries the test of its query. The class ignores whether
// the rule is enabled.
type ruleEval struct {
	// test is the test of the compiled query of an evaluated rule. It is
	// nil for the other classes.
	test func(*inventory.Item) search.Outcome
	// reason is the skip reason of a skipped rule or the block reason of
	// a blocked rule. It is empty for an evaluated rule.
	reason string
	rule   dvm.AutotagRule
	class  ruleClass
}

// evalRules returns the [ruleEval] of each rule of rules, in the same
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
// evalRules calls sources one time, and only when a rule of rules without
// an earlier problem reads a game mode.
func evalRules(
	s *inventory.Snapshot,
	rules []dvm.AutotagRule,
	sources func() map[string]rolls.RatingSource,
) []ruleEval {
	evals := make([]ruleEval, len(rules))
	queries := make([]search.CompiledQuery, len(rules))
	rated := false

	for i, rule := range rules {
		evals[i].rule = rule

		root, err := search.ParseQuery(rule.Query)
		if err != nil {
			var queryErr *search.QueryError
			if errors.As(err, &queryErr) && queryErr.WebClientRuns() {
				evals[i].class = ruleBlocked
				evals[i].reason = "the web client runs this rule, and the server cannot evaluate it: " + err.Error()
			} else {
				evals[i].class = ruleSkipped
				evals[i].reason = err.Error()
			}

			continue
		}

		query := search.CompileQuery(root)

		switch {
		case len(query.Invalid) > 0:
			evals[i].class = ruleSkipped
			evals[i].reason = "the web client rejects " + strings.Join(query.Invalid, ", ")

		case len(query.Unsupported) > 0:
			evals[i].class = ruleBlocked
			evals[i].reason = "the server does not evaluate " + strings.Join(query.Unsupported, ", ")

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
		if evals[i].class != ruleEvaluated {
			continue
		}

		reason := queries[i].UnratedReason(ratings)
		if reason != "" {
			evals[i].class = ruleBlocked
			evals[i].reason = reason

			continue
		}

		evals[i].test = queries[i].Test(sctx)
	}

	return evals
}

// newMatches is what [firstMatch] gives the new items. Each list follows
// the order of the items that firstMatch reads.
type newMatches struct {
	// taken holds the items that each rule takes, at the index of its
	// [ruleEval].
	taken [][]*inventory.Item
	// undecided holds the items that an enabled rule leaves undecided
	// before any rule matches them.
	undecided []undecidedAt
	// reaching holds the items that reach the first enabled rule that
	// blocks.
	reaching []*inventory.Item
	// untaken holds the items that no rule takes. It is empty when an
	// enabled rule blocks.
	untaken []*inventory.Item
	// blocked is the index of the [ruleEval] of the first enabled rule
	// that blocks, or -1 when no enabled rule blocks.
	blocked int
	// offered is the number of items that firstMatch reads.
	offered int
}

// undecidedAt is a new item that an enabled rule leaves undecided, with
// the index of the [ruleEval] of the rule and the reasons of the outcome.
type undecidedAt struct {
	item    *inventory.Item
	reasons []string
	rule    int
}

// firstMatch gives each item of items to the first enabled rule of evals,
// in order, whose query matches the item, as the web client does. For
// each item, it walks the enabled rules. It passes a skipped rule, and it
// stops at a blocked rule, because that rule might take the item. It
// stops at a rule that leaves the item undecided, because that rule might
// take the item too.
func firstMatch(evals []ruleEval, items []*inventory.Item) newMatches {
	m := newMatches{
		taken:   make([][]*inventory.Item, len(evals)),
		blocked: slices.IndexFunc(evals, func(e ruleEval) bool { return e.rule.Enabled && e.class == ruleBlocked }),
		offered: len(items),
	}

	walked := evals
	if m.blocked >= 0 {
		walked = evals[:m.blocked]
	}

items:
	for _, item := range items {
		for i, e := range walked {
			if !e.rule.Enabled || e.class != ruleEvaluated {
				continue
			}

			got := e.test(item)

			switch got.State {
			case search.Match:
				m.taken[i] = append(m.taken[i], item)

				continue items

			case search.Undecided:
				m.undecided = append(m.undecided, undecidedAt{item: item, reasons: got.Reasons, rule: i})

				continue items

			case search.NoMatch:
			}
		}

		if m.blocked >= 0 {
			m.reaching = append(m.reaching, item)
		} else {
			m.untaken = append(m.untaken, item)
		}
	}

	return m
}

// ruleCount is what [ownedCounts] gives one live rule.
type ruleCount struct {
	// reasons holds each distinct reason of the undecided items, in the
	// order of the items.
	reasons []string
	eval    ruleEval
	// matches is the number of items that the rule matches among the
	// items that no earlier enabled rule took.
	matches int
	// undecided is the number of those items whose outcome the server
	// cannot decide.
	undecided int
	// unknown marks an evaluated or blocked rule after an enabled rule
	// that blocks or that leaves an item undecided. The server then
	// cannot tell which items reach the rule, so it counts nothing for
	// the rule.
	unknown bool
}

// ownedCounts counts the items of items that each rule of evals matches,
// as the rule editor of the web client counts them. It walks the rules in
// order and counts each evaluated rule over the items that no earlier
// enabled rule took. An enabled rule then takes the items that it
// matches, and a disabled rule takes nothing. A skipped rule counts
// nothing and takes nothing, wherever it stands.
//
// The server cannot tell which items an enabled rule takes when the rule
// blocks or leaves an item undecided. ownedCounts therefore marks each
// later rule of the evaluated or the blocked class as unknown.
func ownedCounts(evals []ruleEval, items []*inventory.Item) []ruleCount {
	counts := make([]ruleCount, len(evals))
	taken := map[string]bool{}
	unknown := false

	for i, e := range evals {
		c := &counts[i]
		c.eval = e

		switch {
		case e.class == ruleSkipped:
			continue

		case unknown:
			c.unknown = true

			continue

		case e.class == ruleBlocked:
			unknown = e.rule.Enabled

			continue
		}

		var matched []string

		for _, item := range items {
			if taken[item.InstanceID] {
				continue
			}

			got := e.test(item)

			switch got.State {
			case search.Match:
				matched = append(matched, item.InstanceID)
			case search.Undecided:
				c.undecided++

				for _, reason := range got.Reasons {
					c.reasons = appendNew(c.reasons, reason)
				}

			case search.NoMatch:
			}
		}

		c.matches = len(matched)

		if e.rule.Enabled {
			for _, id := range matched {
				taken[id] = true
			}

			unknown = c.undecided > 0
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

// ruleTag is one new item that an enabled rule with a tag takes.
type ruleTag struct {
	item *inventory.Item
	rule dvm.AutotagRule
}

// ruleTags returns each new item of m that a rule with a tag takes, in
// rule order and then in the order of the items. A rule without a tag
// takes its items and tags none of them.
func ruleTags(evals []ruleEval, m newMatches) []ruleTag {
	var tags []ruleTag

	for i, e := range evals {
		if e.rule.TagID == "" {
			continue
		}

		for _, item := range m.taken[i] {
			tags = append(tags, ruleTag{item: item, rule: e.rule})
		}
	}

	return tags
}

// vaultItem is one new item that an enabled rule with the vault flag
// takes. Rule is the rule that takes the item. Reason keeps the item in
// place, as [vaultBlock] gives it, and is empty for an item that
// run_autotag_rules moves to the vault. Err is the error of a move that
// failed. It is nil after a move that succeeded and for an item with a
// reason.
type vaultItem struct {
	item   *inventory.Item
	err    error
	reason string
	rule   dvm.AutotagRule
}

// vaultItems returns the new items of m that the rules of evals with the
// vault flag take, in the order of [inventory.CompareIDs], each with its
// rule and the reason of [vaultBlock]. Defs holds the definition of each
// item.
func vaultItems(evals []ruleEval, m newMatches, defs *definitions.Defs) []vaultItem {
	var vaulted []vaultItem

	for i, e := range evals {
		if !e.rule.Vault {
			continue
		}

		for _, item := range m.taken[i] {
			vaulted = append(vaulted, vaultItem{
				item:   item,
				reason: vaultBlock(item, defs.Items[item.Hash]),
				rule:   e.rule,
			})
		}
	}

	slices.SortStableFunc(vaulted, func(a, b vaultItem) int {
		return inventory.CompareIDs(a.item.InstanceID, b.item.InstanceID)
	})

	return vaulted
}

// vaultBlock returns the reason that keeps item in place when an autotag
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
// vaultBlock leaves such an item in place with a reason of its own, which
// says that the web client tries the transfer.
func vaultBlock(item *inventory.Item, def definitions.ItemDef) string {
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
