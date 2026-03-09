# Extracts an inventory of all explicitly installed packages from every host
# configuration. Returns a JSON string (use `nix eval --raw .#inventory`).
#
# Each host entry contains:
#   - nixPackages: packages from home.packages
#   - programs: home-manager programs with enable = true
#   - homebrewCasks/homebrewBrews: Homebrew entries (darwin only)
{ self, lib }:
let
  # Strip Nix string context so values serialize cleanly without
  # dragging in store path references that crash the JSON serializer.
  clean = builtins.unsafeDiscardStringContext;

  # Normalize meta.license (single attrset or list) into a comma-separated
  # SPDX string, falling back to shortName for non-standard licenses.
  licenseName =
    l:
    if builtins.isAttrs l then
      clean (l.spdxId or (l.shortName or ""))
    else if builtins.isList l then
      builtins.concatStringsSep ", " (map licenseName l)
    else
      "";

  # Extract { name, homepage, description, license } from a derivation.
  pkgMeta =
    p:
    let
      rawName = p.pname or (p.name or "unknown");
      rawHomepage = if p ? meta && p.meta ? homepage then p.meta.homepage else "";
      rawDesc = if p ? meta && p.meta ? description then p.meta.description else "";
      rawLicense = if p ? meta && p.meta ? license then licenseName p.meta.license else "";
    in
    {
      name = clean rawName;
      homepage = clean rawHomepage;
      description = clean rawDesc;
      license = rawLicense;
    };

  # Extract enabled programs from a home-manager config's programs attrset.
  # Uses tryEval to skip removed/broken modules.
  extractPrograms =
    programs:
    let
      names = builtins.attrNames programs;
      tryProgram =
        name:
        let
          hasEnable = builtins.tryEval (programs.${name} ? enable);
          tryEnabled =
            if hasEnable.success && hasEnable.value then
              builtins.tryEval (builtins.deepSeq programs.${name}.enable programs.${name}.enable)
            else
              {
                success = false;
                value = false;
              };
        in
        if tryEnabled.success && tryEnabled.value then
          let
            v = programs.${name};
            hasPkg = builtins.tryEval (v ? package && v.package != null);
            pkg = if hasPkg.success && hasPkg.value then v.package else null;
            rawHomepage = if pkg != null && pkg ? meta && pkg.meta ? homepage then pkg.meta.homepage else "";
            rawDesc = if pkg != null && pkg ? meta && pkg.meta ? description then pkg.meta.description else "";
            rawLicense =
              if pkg != null && pkg ? meta && pkg.meta ? license then licenseName pkg.meta.license else "";
          in
          [
            {
              inherit name;
              homepage = clean rawHomepage;
              description = clean rawDesc;
              license = rawLicense;
            }
          ]
        else
          [ ];
    in
    lib.sort (a: b: a.name < b.name) (builtins.concatMap tryProgram names);

  # True if a derivation looks like a real, explicitly installed package rather
  # than an internal build artifact. Real nixpkgs packages almost always carry
  # at least a homepage or description; generated helpers (dummy-fc-dir,
  # hm-session-vars, stylix themes, etc.) never do.
  isRealPackage =
    p:
    (p ? meta && p.meta ? homepage && p.meta.homepage != "")
    || (p ? meta && p.meta ? description && p.meta.description != "");

  # Extract { name, homepage, description } from home.packages, sorted.
  # Filters out internal build artifacts that lack any meaningful metadata.
  extractPackages =
    packages: lib.sort (a: b: a.name < b.name) (map pkgMeta (builtins.filter isRealPackage packages));

  # Extract Homebrew cask/brew names (strings), sorted.
  extractBrewNames =
    items:
    lib.sort (a: b: a < b) (
      map (c: clean (if builtins.isString c then c else c.name or "unknown")) items
    );

  # Build inventory for a home-manager config.
  hmInventory = hmCfg: {
    nixPackages = extractPackages hmCfg.home.packages;
    programs = extractPrograms hmCfg.programs;
  };

  # Darwin hosts: HM nested under home-manager.users.<username>.
  darwinInventory =
    _: cfg:
    let
      inherit (cfg.config.dotfiles.system) username;
      hmCfg = cfg.config.home-manager.users.${username};
    in
    hmInventory hmCfg
    // {
      homebrewCasks = extractBrewNames (cfg.config.homebrew.casks or [ ]);
      homebrewBrews = extractBrewNames (cfg.config.homebrew.brews or [ ]);
    };

  # NixOS hosts: same nesting as darwin.
  nixosInventory =
    _: cfg:
    let
      users = cfg.config.home-manager.users or { };
      username = builtins.head (builtins.attrNames users);
    in
    hmInventory users.${username};

  # Standalone home-manager: config is the HM config directly.
  homeInventory = _: cfg: hmInventory cfg.config;

  inventory =
    lib.mapAttrs darwinInventory (self.darwinConfigurations or { })
    // lib.mapAttrs nixosInventory (self.nixosConfigurations or { })
    // lib.mapAttrs homeInventory (self.homeConfigurations or { });
in
builtins.toJSON inventory
