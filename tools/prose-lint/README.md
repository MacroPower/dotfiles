# prose-lint

`prose-lint-rs` lints prose with harper-core. It loads the curated dictionary
and builds an empty lint group, and `prose-lint-rs --version` prints the
harper-core version. It does not lint any file yet.

## Building

To build the package, run `nix build .#prose-lint-rs` from the root of the
repository. The package links the harper of `pkgs.harper`, with the patches
and the embedded dictionary of `harperOverlay` in `lib/default.nix`.

The crate depends on `harper-core` and `harper-comments` by path, because
crates.io does not hold `harper-comments` at the version of `pkgs.harper`.
Cargo reads both crates from `./harper`, which git ignores. To build the crate
outside of Nix, run these commands in `tools/prose-lint`:

```sh
git clone --depth 1 --branch v3.3.0 https://github.com/Automattic/harper.git harper
git -C harper apply ../../../pkgs/harper-precomputed-dict.patch
cargo build
```

This build does not embed the dictionary, so the binary builds the dictionary
from its rune files on each start.

## Harper Version

The crate must link the harper version of `pkgs.harper`. Rule authors test
rules with `harper-cli test`, so both tools must read a rule the same way. The
Nix build fails when the version of `harper-core` in `Cargo.lock` differs from
`pkgs.harper.version`.

When nixpkgs bumps harper, follow these steps:

1. Clone the new tag into `./harper` and apply the patch.
2. Set the new version on both dependencies in `Cargo.toml`.
3. Run `cargo build` to update `Cargo.lock`.
