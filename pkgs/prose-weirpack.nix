{
  lib,
  stdenvNoCC,
  harper,
  zip,
  python3,
  gawk,
}:

let
  rulesDir = ../configs/harper/rules;

  # A rule's tier is the directory it lives in: rules/required,
  # rules/recommended, or rules/optional. A `.weir` file is a Weir
  # rule, an `.awk` file is a check prose-lint runs on markdown next
  # to Harper, and `builtins.txt` names the Harper built-ins the tier
  # enables. check.py fails the build when a rule has no fixture pair
  # under fixtures/<tier>, when two tiers list the same built-in, or
  # when a fixture pair has no rule.
  tierOrder = [
    "required"
    "recommended"
    "optional"
  ];

  # Harper names a rule by its archive stem, so two tiers can hold a
  # rule of the same name only if the stems differ. The build renames
  # each Weir rule to `<Name>_<Tier>` inside the weirpack, and
  # prose-lint strips the suffix from each finding after it reads the
  # tier off it. Built-ins keep their bare name, since a built-in can
  # sit in one tier only.
  internalName = name: tier: "${name}_${lib.toSentenceCase tier}";

  entries = lib.attrNames (builtins.readDir rulesDir);

  tierFiles =
    tier:
    let
      dir = rulesDir + "/${tier}";
      listing = if builtins.pathExists dir then builtins.attrNames (builtins.readDir dir) else [ ];
      stems = suffix: map (lib.removeSuffix suffix) (lib.filter (lib.hasSuffix suffix) listing);
      builtinsFile = dir + "/builtins.txt";
    in
    {
      inherit tier;
      weir = stems ".weir";
      awk = stems ".awk";
      builtin =
        if builtins.pathExists builtinsFile then
          lib.filter (l: l != "" && !lib.hasPrefix "#" l) (
            map lib.trim (lib.splitString "\n" (builtins.readFile builtinsFile))
          )
        else
          [ ];
    };

  tiersList = map tierFiles tierOrder;

  # Internal Weir names, in tier order, for the `--only` list.
  ruleNames = lib.concatMap (t: map (name: internalName name t.tier) t.weir) tiersList;

  builtinRules = lib.concatMap (t: t.builtin) tiersList;

  # One entry per awk check: the internal name prose-lint tags it
  # with, and its path under $out.
  awkChecks = lib.concatMap (
    t:
    map (name: {
      inherit name;
      inherit (t) tier;
      internal = internalName name t.tier;
      path = "share/harper/awk/${t.tier}/${name}.awk";
    }) t.awk
  ) tiersList;

  # Internal rule name to tier label, the map prose-lint embeds.
  tiers = lib.listToAttrs (
    lib.concatMap (
      t:
      let
        label = lib.toSentenceCase t.tier;
      in
      map (name: lib.nameValuePair (internalName name t.tier) label) (t.weir ++ t.awk)
      ++ map (name: lib.nameValuePair name label) t.builtin
    ) tiersList
  );

  manifest = builtins.toJSON {
    author = "Jacob Colvin";
    version = "0.1.0";
    description = "Mechanical half of the prose skill";
    license = "Apache-2.0";
  };
in
assert lib.assertMsg (lib.all (
  e: lib.elem e tierOrder
) entries) "configs/harper/rules holds an entry outside the tier directories: ${toString entries}";
stdenvNoCC.mkDerivation {
  pname = "prose-weirpack";
  version = "0.1.0";

  src = ../configs/harper;

  nativeBuildInputs = [ zip ];
  nativeCheckInputs = [
    harper
    python3
    gawk
  ];

  # Harper reads a rule's name from the archive entry's stem, so the
  # build copies each rule to a flat directory under its internal name
  # and zips that directory (`-j`). Pinned mtimes keep the zip
  # reproducible.
  buildPhase = ''
    runHook preBuild
    mkdir pack
    for tier in ${lib.escapeShellArgs tierOrder}; do
      label="$(printf '%s' "''${tier:0:1}" | tr '[:lower:]' '[:upper:]')''${tier:1}"
      for rule in rules/"$tier"/*.weir; do
        [ -e "$rule" ] || continue
        name=$(basename "$rule" .weir)
        cp "$rule" "pack/''${name}_''${label}.weir"
      done
    done
    printf '%s' ${lib.escapeShellArg manifest} > pack/manifest.json
    find pack -exec touch -d "@$SOURCE_DATE_EPOCH" {} +
    zip -X -j -q prose.weirpack pack/*.weir pack/manifest.json
    runHook postBuild
  '';

  doCheck = true;

  # harper-cli looks for a user dictionary under $HOME and prints a
  # note when none exists; pointing HOME at the build dir keeps that
  # lookup inside the sandbox.
  checkPhase = ''
    runHook preCheck
    export HOME="$TMPDIR"
    for rule in rules/*/*.weir; do
      harper-cli test --no-color "$rule"
    done
    python3 check.py prose.weirpack fixtures rules
    runHook postCheck
  '';

  installPhase = ''
    runHook preInstall
    install -D -m 0644 prose.weirpack "$out/share/harper/prose.weirpack"
    ${lib.concatMapStringsSep "\n" (
      c: ''install -D -m 0644 rules/${c.tier}/${c.name}.awk "$out/${c.path}"''
    ) awkChecks}
    runHook postInstall
  '';

  passthru = {
    inherit
      ruleNames
      builtinRules
      awkChecks
      tiers
      ;
  };

  meta = {
    description = "Weir rules that enforce the mechanical half of the prose skill";
    license = lib.licenses.asl20;
    platforms = lib.platforms.all;
  };
}
