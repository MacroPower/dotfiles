package dvm_test

import (
	"cmp"
	"testing"

	"github.com/stretchr/testify/assert"

	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/dvm"
)

func TestTagChanges(t *testing.T) {
	t.Parallel()

	rows := []dvm.ItemTag{
		{InstanceID: "1001", ItemHash: 100, TagID: "keep"},
		{InstanceID: "1001", ItemHash: 100, TagID: dvm.TagFavorite},
		{InstanceID: "1001", ItemHash: 100, TagID: "pvp", Removed: true},
		{InstanceID: "1002", ItemHash: 101, TagID: "junk"},
		{InstanceID: "1003", ItemHash: 100, TagID: "junk", RuleID: "rule-1"},
	}

	tests := map[string]struct {
		// instance is the instance id of the item. It defaults to 1001.
		instance string
		tag      string
		// rule is the id of the autotag rule that sets the tag. It is
		// empty for a tag that a person sets.
		rule string
		want []dvm.ItemTag
	}{
		"replace the tag": {
			tag: "junk",
			want: []dvm.ItemTag{
				{InstanceID: "1001", ItemHash: 100, TagID: "keep", Removed: true},
				{InstanceID: "1001", ItemHash: 100, TagID: "junk"},
			},
		},
		"clear the tag": {
			tag: "",
			want: []dvm.ItemTag{
				{InstanceID: "1001", ItemHash: 100, TagID: "keep", Removed: true},
			},
		},
		"same tag writes the row again": {
			tag: "keep",
			want: []dvm.ItemTag{
				{InstanceID: "1001", ItemHash: 100, TagID: "keep"},
			},
		},
		// Only the added row carries the rule id, as in the web client.
		"a rule replaces the tag": {
			tag:  "junk",
			rule: "rule-1",
			want: []dvm.ItemTag{
				{InstanceID: "1001", ItemHash: 100, TagID: "keep", Removed: true},
				{InstanceID: "1001", ItemHash: 100, TagID: "junk", RuleID: "rule-1"},
			},
		},
		"a rule sets the tag that a person set": {
			tag:  "keep",
			rule: "rule-1",
			want: []dvm.ItemTag{
				{InstanceID: "1001", ItemHash: 100, TagID: "keep", RuleID: "rule-1"},
			},
		},
		"a person sets the tag that a rule set": {
			instance: "1003",
			tag:      "junk",
			want: []dvm.ItemTag{
				{InstanceID: "1003", ItemHash: 100, TagID: "junk"},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			instance := cmp.Or(tc.instance, "1001")
			assert.Equal(t, tc.want, dvm.TagChanges(rows, instance, 100, tc.tag, tc.rule))
		})
	}
}
