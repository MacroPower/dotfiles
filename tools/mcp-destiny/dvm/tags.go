package dvm

import "slices"

// TagFavorite is the tag id that marks a favorite. An item carries it
// beside its one ordinary tag.
const TagFavorite = "favorite"

// TagJunk is the tag id that marks an item for dismantling.
const TagJunk = "junk"

// TagOrder lists the tag ids that the web client defines, in its display
// order. The web client ignores stored tag definitions. Callers must not
// modify the slice.
var TagOrder = []string{"keep", "pvp", "pve", "junk", "infuse", "archive"}

// PrimaryTag returns the one ordinary tag that the web client shows for an
// item. Storage allows several, so the web client display order decides.
func PrimaryTag(ids []string) string {
	for _, id := range TagOrder {
		if slices.Contains(ids, id) {
			return id
		}
	}

	rest := slices.DeleteFunc(slices.Clone(ids), func(id string) bool { return id == TagFavorite })
	if len(rest) == 0 {
		return ""
	}

	return slices.Min(rest)
}

// TagChanges returns the rows that leave an item with tagID as its one
// ordinary tag. An empty tagID leaves the item untagged. The favorite tag
// is independent of the ordinary tag, so TagChanges keeps it. The row that
// adds tagID carries ruleID, the id of the autotag rule that sets the tag.
// A tag that a person sets has an empty ruleID.
func TagChanges(rows []ItemTag, instanceID string, itemHash uint32, tagID, ruleID string) []ItemTag {
	var changes []ItemTag

	for _, row := range rows {
		if row.InstanceID != instanceID || row.Removed || row.TagID == tagID || row.TagID == TagFavorite {
			continue
		}

		changes = append(changes, ItemTag{
			InstanceID: instanceID,
			ItemHash:   itemHash,
			TagID:      row.TagID,
			Removed:    true,
		})
	}

	if tagID != "" {
		changes = append(changes, ItemTag{InstanceID: instanceID, ItemHash: itemHash, TagID: tagID, RuleID: ruleID})
	}

	return changes
}
