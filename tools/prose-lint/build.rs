//! Embeds the prose rules of `rules/` in the binary.
//!
//! The script writes `rules.rs` to `OUT_DIR`, which lists the source of
//! each Weir rule and the names in each `builtins.txt`, by tier.

use std::env;
use std::fmt::Write as _;
use std::fs;
use std::path::Path;

const TIERS: [(&str, &str); 3] = [
    ("required", "Required"),
    ("recommended", "Recommended"),
    ("optional", "Optional"),
];

fn main() {
    let rules = Path::new(env!("CARGO_MANIFEST_DIR")).join("rules");
    let mut weir = String::new();
    let mut builtins = String::new();
    for (dir, tier) in TIERS {
        let dir = rules.join(dir);
        println!("cargo:rerun-if-changed={}", dir.display());
        let mut names: Vec<_> = fs::read_dir(&dir)
            .unwrap_or_else(|e| panic!("reading {}: {e}", dir.display()))
            .map(|entry| entry.expect("listing a rules directory").path())
            .collect();
        names.sort();
        for path in names {
            let file = path
                .file_name()
                .and_then(|n| n.to_str())
                .expect("rule file names are UTF-8");
            if let Some(name) = file.strip_suffix(".weir") {
                println!("cargo:rerun-if-changed={}", path.display());
                writeln!(
                    weir,
                    "    ({name:?}, Tier::{tier}, include_str!({:?})),",
                    path.display().to_string()
                )
                .expect("writing to a String");
            } else if file == "builtins.txt" {
                println!("cargo:rerun-if-changed={}", path.display());
                let text = fs::read_to_string(&path)
                    .unwrap_or_else(|e| panic!("reading {}: {e}", path.display()));
                for name in text.lines().map(str::trim) {
                    if !name.is_empty() && !name.starts_with('#') {
                        writeln!(builtins, "    ({name:?}, Tier::{tier}),")
                            .expect("writing to a String");
                    }
                }
            }
        }
    }

    let out = Path::new(&env::var_os("OUT_DIR").expect("cargo sets OUT_DIR")).join("rules.rs");
    let code = format!(
        "/// The name, the tier, and the source of each Weir rule.\n\
         pub const WEIR_RULES: &[(&str, Tier, &str)] = &[\n{weir}];\n\n\
         /// The name and the tier of each Harper built-in that a tier enables.\n\
         pub const BUILTINS: &[(&str, Tier)] = &[\n{builtins}];\n"
    );
    fs::write(&out, code).unwrap_or_else(|e| panic!("writing {}: {e}", out.display()));
}
