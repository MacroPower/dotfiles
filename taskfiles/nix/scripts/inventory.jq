# Formats the inventory JSON as deduplicated markdown tables split by platform.
# Packages appearing on both darwin and linux hosts are "Universal"; the rest
# land in Darwin-only or Linux-only sections.  Homebrew casks get their own
# subsection under Darwin.

# Metadata overrides for HM programs whose modules do not expose a package
# with useful meta attributes. Entries with null values are excluded from
# the inventory entirely (configuration-only modules, not standalone tools).
def overrides: {
  "carapace":          { "homepage": "https://carapace.sh",                             "description": "Multi-shell multi-command argument completer",   "license": "MIT" },
  "docker-cli":        { "homepage": "https://www.docker.com",                          "description": "Docker CLI",                                     "license": "Apache-2.0" },
  "nix-index":         { "homepage": "https://github.com/nix-community/nix-index",      "description": "Quickly locate nix packages with specific files", "license": "BSD-3-Clause" },
  "npm":               { "homepage": "https://www.npmjs.com",                            "description": "Package manager for JavaScript",                 "license": "Artistic-2.0" },
  "terminal-notifier": { "homepage": "https://github.com/julienXX/terminal-notifier",   "description": "Send macOS notifications from the terminal",     "license": "MIT" },
  "vim":               { "homepage": "https://www.vim.org",                              "description": "Highly configurable text editor",                "license": "Vim" },
  "exa":               null,
  "man":               null,
  "mcp":               null,
  "ssh":               null
};

# Escape backticks so they don't break markdown table cells.
def md_escape: gsub("`"; "\\`");

def fmt_entry:
  "| " + (if .homepage == "" then .name else "[\(.name)](\(.homepage))" end) +
  " | " + (.description | md_escape) +
  " | " + (.license // "") + " |";

# Pick the best representative from a group of entries that share a homepage:
# shortest name wins, ties broken by longest description.
# Prefer names without -bin/-bin-* suffixes, then shortest name.
def pick_best:
  sort_by(
    (if (.name | test("-bin($|-)")?) then 1 else 0 end),
    (.name | length),
    (- (.description | length))
  ) | first;

# Apply overrides: enrich or exclude entries based on the overrides map.
def apply_overrides:
  .name as $n | overrides as $o |
  if ($o | has($n)) and $o[$n] == null then empty
  elif ($o | has($n)) then
    . + ($o[$n] | to_entries | map(select(.value != "")) | from_entries)
  else .
  end;

# Table header used for each section.
def table_header:
  "| Name | Description | License |\n|------|-------------|--------|";

. as $inv |

# Tag each entry with the platform of its host.
[to_entries[] | .value.platform as $plat |
  (.value.programs[], .value.nixPackages[]) | apply_overrides |
  {entry: ., platform: $plat}
] |

# Deduplicate by name, collecting which platforms each package appears on.
group_by(.entry.name) | map({
  entry: ([.[].entry] | sort_by(- (.homepage|length) - (.description|length) - ((.license // "")|length)) | first),
  platforms: ([.[].platform] | unique)
}) |

# Collapse entries sharing the same non-empty homepage, merging platform sets.
group_by(if .entry.homepage == "" then .entry.name else .entry.homepage end) |
map({
  entry: ([.[].entry] | pick_best),
  platforms: ([.[].platforms[]] | unique)
}) |
sort_by(.entry.name) |

# Split into three buckets.
{
  universal: [.[] | select((.platforms | contains(["darwin"])) and (.platforms | contains(["linux"]))) | .entry],
  darwin:    [.[] | select((.platforms | contains(["darwin"])) and ((.platforms | contains(["linux"])) | not)) | .entry],
  linux:     [.[] | select((.platforms | contains(["linux"])) and ((.platforms | contains(["darwin"])) | not)) | .entry]
} |

# Homebrew casks across all hosts, deduplicated.
. as $grouped | [$inv[] | .homebrewCasks // [] | .[]] | unique | sort | . as $casks |

# Render sections, skipping empty ones.
"## Package Inventory\n\n" +

"### Universal\n\n" + table_header + "\n" +
([$grouped.universal[] | fmt_entry] | join("\n")) +

if ($grouped.darwin | length) > 0 then
  "\n\n### Darwin\n\n" + table_header + "\n" +
  ([$grouped.darwin[] | fmt_entry] | join("\n"))
else "" end +

if ($grouped.linux | length) > 0 then
  "\n\n### Linux\n\n" + table_header + "\n" +
  ([$grouped.linux[] | fmt_entry] | join("\n"))
else "" end +

if ($casks | length) > 0 then
  "\n\n### Homebrew Casks\n\n| Name |\n|------|\n" +
  ([$casks[] | "| [" + . + "](https://formulae.brew.sh/cask/" + . + ") |"] | join("\n"))
else "" end
