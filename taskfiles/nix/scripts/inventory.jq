# Formats the inventory JSON as a single deduplicated markdown table.
# Merges all hosts, preferring entries with the most metadata and the
# shortest/cleanest name when multiple packages share the same homepage
# (e.g. firefox/firefox-bin, ghostty/ghostty-bin, gpg/gnupg).

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

. as $inv |

# Collect all entries, apply overrides, deduplicate by name first.
[$inv[] | .programs[], .nixPackages[] | apply_overrides] |
  group_by(.name) |
  map(sort_by(- (.homepage | length) - (.description | length) - ((.license // "") | length)) | first) |

  # Then collapse entries that share the same non-empty homepage.
  group_by(if .homepage == "" then .name else .homepage end) |
  map(pick_best) |
  sort_by(.name) |
  . as $entries |

# All homebrew casks across hosts, deduplicated.
[$inv[] | .homebrewCasks // [] | .[]] |
  unique | sort |
  . as $casks |

"## Package Inventory\n\n" +
"| Name | Description | License |\n" +
"|------|-------------|--------|\n" +
([$entries[] | fmt_entry] | join("\n")) +

if ($casks | length) > 0 then
  "\n\n## Homebrew Casks\n\n| Name |\n|------|\n" +
  ([$casks[] | "| [" + . + "](https://formulae.brew.sh/cask/" + . + ") |"] | join("\n"))
else "" end
