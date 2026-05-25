{
  pkgs,
  lib,
  config,
  ...
}:

let
  cfg = config.dotfiles.photo-cli;
  sopsEnabled = config.dotfiles.sops.enable;

  secretPath =
    name: if sopsEnabled then config.sops.secrets.${name}.path else "/run/secrets/disabled";

  exportSecret = envVar: secretName: ''
    if [ -f "${secretPath secretName}" ]; then
      export ${envVar}="$(cat "${secretPath secretName}" 2>/dev/null || true)"
    fi
  '';

  # Convert a Nix value into the string form the .NET ConfigurationBinder
  # accepts. Booleans must be the literals "true"/"false" -- `toString true`
  # in Nix yields "1" and would silently fail to bind to bool?.
  valueToString = v: if builtins.isBool v then lib.boolToString v else toString v;

  # Flatten { LogLevel = { Default = "Warning"; PhotoCli = "Information"; }; }
  # into  { "LogLevel__Default" = "Warning"; "LogLevel__PhotoCli" = "Information"; }
  # and { SupportedExtensions = [ "jpg" "jpeg" ]; } into
  #     { "SupportedExtensions__0" = "jpg"; "SupportedExtensions__1" = "jpeg"; }.
  # Only one level of attrs is flattened -- matches the photo-cli schema
  # (`LogLevel : Dictionary<string,string>` is the only nested field).
  flattenSettings =
    attrs:
    lib.foldlAttrs (
      acc: k: v:
      if v == null then
        acc
      else if builtins.isAttrs v then
        acc // lib.mapAttrs' (subK: subV: lib.nameValuePair "${k}__${subK}" (valueToString subV)) v
      else if builtins.isList v then
        acc
        // lib.listToAttrs (lib.imap0 (i: x: lib.nameValuePair "${k}__${toString i}" (valueToString x)) v)
      else
        acc // { ${k} = valueToString v; }
    ) { } attrs;

  # Microsoft.Extensions.Logging level names. photo-cli passes log-level
  # strings straight through to that library, so the enum mirrors its
  # public LogLevel values.
  logLevelEnum = lib.types.enum [
    "Trace"
    "Debug"
    "Information"
    "Warning"
    "Error"
    "Critical"
    "None"
  ];

  # Wraps a typed scalar in `nullOr` with `default = null` so an unset
  # option contributes nothing after `flattenSettings`, leaving photo-cli
  # to apply its own constructor default. `LogLevel` doesn't use this --
  # an empty attrset is its natural "unset".
  settingOpt =
    type: description:
    lib.mkOption {
      type = lib.types.nullOr type;
      default = null;
      inherit description;
    };

  # Enum literal sets for photo-cli verb flags. Sourced from
  # photo-cli's `Models/Enums/*.cs`. The `Unset = 0` sentinel members
  # are excluded -- those exist so photo-cli's constructor can detect
  # "no CLI value supplied" and fall back to its own default; passing
  # them as a CLI value would defeat that. CommandLineParser binds
  # enums case-insensitively (Enum.Parse(..., ignoreCase: true)) but
  # `lib.types.enum` is exact-match, so users must spell the literal
  # using the C# member casing.
  namingStyleEnum = lib.types.enum [
    "Numeric"
    "Day"
    "DateTimeWithMinutes"
    "DateTimeWithSeconds"
    "Address"
    "DayAddress"
    "DateTimeWithMinutesAddress"
    "DateTimeWithSecondsAddress"
    "AddressDay"
    "AddressDateTimeWithMinutes"
    "AddressDateTimeWithSeconds"
  ];
  folderProcessTypeEnum = lib.types.enum [
    "Single"
    "SubFoldersPreserveFolderHierarchy"
    "FlattenAllSubFolders"
  ];
  numberNamingTextStyleEnum = lib.types.enum [
    "AllNamesAreSameLength"
    "PaddingZeroCharacter"
    "OnlySequentialNumbers"
  ];
  copyInvalidFormatActionEnum = lib.types.enum [
    "Continue"
    "PreventProcess"
    "DontCopyToOutput"
    "InSubFolder"
  ];
  copyNoPhotoTakenDateActionEnum = lib.types.enum [
    "Continue"
    "PreventProcess"
    "DontCopyToOutput"
    "InSubFolder"
    "AppendToEndOrderByFileName"
    "InsertToBeginningOrderByFileName"
  ];
  copyNoCoordinateActionEnum = lib.types.enum [
    "Continue"
    "PreventProcess"
    "DontCopyToOutput"
    "InSubFolder"
  ];
  groupByFolderTypeEnum = lib.types.enum [
    "YearMonthDay"
    "YearMonth"
    "Year"
    "AddressFlat"
    "AddressHierarchy"
  ];
  folderAppendTypeEnum = lib.types.enum [
    "FirstYearMonthDay"
    "FirstYearMonth"
    "FirstYear"
    "DayRange"
    "MatchingMinimumAddress"
  ];
  folderAppendLocationTypeEnum = lib.types.enum [
    "Prefix"
    "Suffix"
  ];
  # ReverseGeocodeProvider's byte value 4 is reserved in the C# enum
  # (formerly MapQuest); the literal list below is complete.
  reverseGeocodeProviderEnum = lib.types.enum [
    "Disabled"
    "BigDataCloud"
    "OpenStreetMapFoundation"
    "GoogleMaps"
    "LocationIq"
  ];
  missingReverseGeocodeEnum = lib.types.enum [
    "Continue"
    "PreventProcess"
  ];
  archiveInvalidFormatActionEnum = lib.types.enum [
    "Continue"
    "PreventProcess"
  ];
  archiveNoPhotoTakenDateActionEnum = lib.types.enum [
    "Continue"
    "PreventProcess"
  ];
  archiveNoCoordinateActionEnum = lib.types.enum [
    "Continue"
    "PreventProcess"
  ];
  archiveAlbumTypeEnum = lib.types.enum [
    "NoAlbumLinking"
    "Individual"
    "DateRange"
  ];
  infoInvalidFormatActionEnum = lib.types.enum [
    "Continue"
    "PreventProcess"
  ];
  infoNoPhotoTakenDateActionEnum = lib.types.enum [
    "Continue"
    "PreventProcess"
  ];
  infoNoCoordinateActionEnum = lib.types.enum [
    "Continue"
    "PreventProcess"
  ];
  addressListTypeEnum = lib.types.enum [
    "AllAvailableProperties"
    "SelectedProperties"
    "FullResponse"
  ];

  mkFlag =
    {
      long,
      short,
      kind ? "scalar",
      type,
      description ? null,
    }:
    {
      inherit
        long
        short
        kind
        type
        ;
      description =
        if description != null then description else "Default value for `--${long}` / `-${short}`.";
    };

  # Reverse-geocode flags shared across copy/archive/info via `//`.
  # Address spec cherry-picks the non-`reverseGeocode` entries instead
  # because its `reverseGeocode` is a required positional rather than
  # a shared optional. API-key flags (`--bigdatacloud-key`,
  # `--googlemaps-key`, `--locationiq-key`) are intentionally absent
  # everywhere -- those values come from sops via
  # `dotfiles.photo-cli.geocoder.*`, never from plain-text Nix.
  sharedGeocodeFlags = {
    reverseGeocode = mkFlag {
      long = "reverse-geocode";
      short = "e";
      type = reverseGeocodeProviderEnum;
    };
    bigDataCloudAdminLevels = mkFlag {
      long = "bigdatacloud-levels";
      short = "u";
      kind = "list";
      type = lib.types.listOf lib.types.int;
    };
    googleMapsAddressTypes = mkFlag {
      long = "googlemaps-types";
      short = "m";
      kind = "list";
      type = lib.types.listOf lib.types.str;
    };
    openStreetMapProperties = mkFlag {
      long = "openstreetmap-properties";
      short = "r";
      kind = "list";
      type = lib.types.listOf lib.types.str;
    };
    hasPaidLicense = mkFlag {
      long = "has-paid-license";
      short = "h";
      kind = "bool";
      type = lib.types.bool;
    };
    language = mkFlag {
      long = "language";
      short = "l";
      type = lib.types.str;
    };
  };

  # Excluded by design:
  # - Run-specific paths/IDs: --input, --output, --album-name,
  #   --update-album. They vary per invocation so a "default" is
  #   meaningless.
  # - Flags duplicated by `ToolOptionsRaw`: --expected-day-range
  #   (mirrored as `settings.ExpectedDayRange`) and the API-key flags
  #   (`geocoder.*` + sops). One binding path per knob.
  verbSpecs = {
    copy = {
      namingStyle = mkFlag {
        long = "naming-style";
        short = "s";
        type = namingStyleEnum;
      };
      folderProcessType = mkFlag {
        long = "process-type";
        short = "f";
        type = folderProcessTypeEnum;
      };
      numberNamingTextStyle = mkFlag {
        long = "number-style";
        short = "n";
        type = numberNamingTextStyleEnum;
      };
      invalidFormatAction = mkFlag {
        long = "invalid-format";
        short = "x";
        type = copyInvalidFormatActionEnum;
      };
      noPhotoTakenDateAction = mkFlag {
        long = "no-taken-date";
        short = "t";
        type = copyNoPhotoTakenDateActionEnum;
      };
      noCoordinateAction = mkFlag {
        long = "no-coordinate";
        short = "c";
        type = copyNoCoordinateActionEnum;
      };
      dryRun = mkFlag {
        long = "dry-run";
        short = "d";
        kind = "bool";
        type = lib.types.bool;
      };
      groupByFolderType = mkFlag {
        long = "group-by";
        short = "g";
        type = groupByFolderTypeEnum;
      };
      folderAppendType = mkFlag {
        long = "folder-append";
        short = "a";
        type = folderAppendTypeEnum;
      };
      folderAppendLocationType = mkFlag {
        long = "folder-append-location";
        short = "p";
        type = folderAppendLocationTypeEnum;
      };
      verify = mkFlag {
        long = "verify";
        short = "v";
        kind = "bool";
        type = lib.types.bool;
      };
      missingReverseGeocode = mkFlag {
        long = "missing-reverse-geocode";
        short = "z";
        type = missingReverseGeocodeEnum;
      };
    }
    // sharedGeocodeFlags;

    archive = {
      dryRun = mkFlag {
        long = "dry-run";
        short = "d";
        kind = "bool";
        type = lib.types.bool;
      };
      invalidFormatAction = mkFlag {
        long = "invalid-format";
        short = "x";
        type = archiveInvalidFormatActionEnum;
      };
      noPhotoTakenDateAction = mkFlag {
        long = "no-taken-date";
        short = "t";
        type = archiveNoPhotoTakenDateActionEnum;
      };
      noCoordinateAction = mkFlag {
        long = "no-coordinate";
        short = "c";
        type = archiveNoCoordinateActionEnum;
      };
      albumType = mkFlag {
        long = "album-type";
        short = "y";
        type = archiveAlbumTypeEnum;
      };
      autoReverseGeocodeAlbum = mkFlag {
        long = "auto-reverse-geocode-album";
        short = "s";
        kind = "bool";
        type = lib.types.bool;
      };
      deleteOnSource = mkFlag {
        long = "delete-on-source";
        short = "f";
        kind = "bool";
        type = lib.types.bool;
      };
      missingReverseGeocode = mkFlag {
        long = "missing-reverse-geocode";
        short = "z";
        type = missingReverseGeocodeEnum;
      };
    }
    // sharedGeocodeFlags;

    info = {
      allFolders = mkFlag {
        long = "all-folders";
        short = "a";
        kind = "bool";
        type = lib.types.bool;
      };
      invalidFormatAction = mkFlag {
        long = "invalid-format";
        short = "x";
        type = infoInvalidFormatActionEnum;
      };
      noPhotoTakenDateAction = mkFlag {
        long = "no-taken-date";
        short = "t";
        type = infoNoPhotoTakenDateActionEnum;
      };
      noCoordinateAction = mkFlag {
        long = "no-coordinate";
        short = "c";
        type = infoNoCoordinateActionEnum;
      };
      missingReverseGeocode = mkFlag {
        long = "missing-reverse-geocode";
        short = "z";
        type = missingReverseGeocodeEnum;
      };
    }
    // sharedGeocodeFlags;

    address = {
      reverseGeocode = mkFlag {
        long = "reverse-geocode";
        short = "e";
        type = reverseGeocodeProviderEnum;
      };
      type = mkFlag {
        long = "type";
        short = "t";
        type = addressListTypeEnum;
      };
      inherit (sharedGeocodeFlags)
        bigDataCloudAdminLevels
        googleMapsAddressTypes
        openStreetMapProperties
        hasPaidLicense
        language
        ;
    };
  };

  # Defines `_pc_has_flag <short> <long> "$@"` -- returns 0 iff any of
  # `-<short>`, `--<long>`, `--<long>=*` appears in the args. Pattern
  # set deliberately omits `-<short>=*`: CommandLineParser 2.x doesn't
  # accept that form, so a user passing it would already hit a parser
  # error.
  hasFlagFn = ''
    _pc_has_flag() {
      local short="$1" long="$2"; shift 2
      for a in "$@"; do
        case "$a" in
          "-$short"|"--$long"|"--$long="*) return 0 ;;
        esac
      done
      return 1
    }
  '';

  renderFlag =
    verb: name: meta:
    let
      v = cfg.commandDefaults.${verb}.${name};
      flag = lib.escapeShellArg "--${meta.long}";
      guard =
        "_pc_has_flag ${lib.escapeShellArg meta.short} "
        + ''${lib.escapeShellArg meta.long} "''${args[@]}"'';
    in
    if meta.kind == "bool" then
      lib.optionalString v "if ! ${guard}; then args+=(${flag}); fi"
    else if meta.kind == "list" then
      "if ! ${guard}; then args+=(${flag} ${
        lib.concatMapStringsSep " " (x: lib.escapeShellArg (valueToString x)) v
      }); fi"
    else
      "if ! ${guard}; then args+=(${flag} ${lib.escapeShellArg (valueToString v)}); fi";

  # `args=()` then conditional fill defends against `set -u` choking on
  # empty `("$@")` under older bash; safe under bash 5+ too.
  verbBranch =
    verb: spec:
    let
      setFlags = lib.filterAttrs (n: _: cfg.commandDefaults.${verb}.${n} != null) spec;
      lines = lib.mapAttrsToList (n: m: renderFlag verb n m) setFlags;
    in
    lib.optionalString (lines != [ ]) ''
      ${verb})
        shift
        args=()
        [ "$#" -gt 0 ] && args=("$@")
        ${lib.concatStringsSep "\n        " lines}
        set -- ${verb} "''${args[@]}"
        ;;
    '';

  # makeWrapper executes blocks in declaration order: env-var sets,
  # then `--run` blocks, then `exec ... "$@"`. The dispatcher mutates
  # `"$@"` via `set --`; the trailing exec picks up the rewrite. The
  # corollary: this wrapper must never gain `--add-flags`, since those
  # land before `"$@"` and would shove an arg ahead of the verb.
  dispatcher = ''
    ${hasFlagFn}
    case "''${1-}" in
      ${lib.concatStrings (lib.mapAttrsToList verbBranch verbSpecs)}
    esac
  '';

  anyCommandDefault = lib.any (verbAttrs: lib.any (v: v != null) (lib.attrValues verbAttrs)) (
    lib.attrValues cfg.commandDefaults
  );

  flatSettings = flattenSettings cfg.settings;

  # Use --set-default (not --set) so a user-supplied env var still wins over
  # the Nix-declared value. makeWrapper's --set is an unconditional overwrite;
  # --set-default emits the `export $varName=${$varName-...}` form, which only
  # sets the var when it's absent. Aligns with photo-cli's natural "env vars
  # are an override knob" workflow.
  envFlags = lib.concatStringsSep " \\\n" (
    lib.mapAttrsToList (
      k: v: "  --set-default ${lib.escapeShellArg k} ${lib.escapeShellArg v}"
    ) flatSettings
  );

  geocoderRunBlock = lib.concatStrings [
    (lib.optionalString cfg.geocoder.bigDataCloud.enable (
      exportSecret "BigDataCloudApiKey" cfg.geocoder.bigDataCloud.secretName
    ))
    (lib.optionalString cfg.geocoder.googleMaps.enable (
      exportSecret "GoogleMapsApiKey" cfg.geocoder.googleMaps.secretName
    ))
    (lib.optionalString cfg.geocoder.locationIq.enable (
      exportSecret "LocationIqApiKey" cfg.geocoder.locationIq.secretName
    ))
  ];

  anyGeocoder =
    cfg.geocoder.bigDataCloud.enable
    || cfg.geocoder.googleMaps.enable
    || cfg.geocoder.locationIq.enable;

  photoCliWrapped = pkgs.symlinkJoin {
    name = "photo-cli-wrapped-${pkgs.photo-cli.version}";
    pname = "photo-cli";
    inherit (pkgs.photo-cli) version meta;
    paths = [ pkgs.photo-cli ];
    nativeBuildInputs = [ pkgs.makeWrapper ];
    postBuild = ''
            wrapProgram $out/bin/photo-cli \
      ${envFlags}${lib.optionalString anyGeocoder " \\\n  --run ${lib.escapeShellArg geocoderRunBlock}"}${lib.optionalString anyCommandDefault " \\\n  --run ${lib.escapeShellArg dispatcher}"}
    '';
  };
in
{
  options.dotfiles.photo-cli = {
    settings = lib.mkOption {
      default = { };
      description = ''
        Non-secret photo-cli settings. Each option corresponds 1:1 to a
        property on photo-cli's `ToolOptionsRaw` (see
        https://photocli.com/docs/settings) and is bound via env vars by
        the wrapper using `--set-default`, so a shell-supplied env var
        still wins. Unset options (left at `null`, or `{ }` for
        `LogLevel`) fall through to the `ToolOptions(ToolOptionsRaw)`
        constructor defaults; each option's description records that
        constant for reference.

        Note: photo-cli also tries to read `appsettings.json` relative to
        the invocation CWD (not the install dir). If a user's CWD happens
        to contain that file, it overrides env vars including the ones
        the wrapper sets here.

        API keys for reverse geocoders are NOT configured here; enable
        them under `dotfiles.photo-cli.geocoder.*` and provide values via
        sops.
      '';
      type = lib.types.submodule {
        options = {
          YearFormat = settingOpt lib.types.str ''
            .NET datetime format string used for the year segment of
            generated folder names. Constructor default: "yyyy".
          '';

          MonthFormat = settingOpt lib.types.str ''
            .NET datetime format string used for the month segment of
            generated folder names. Constructor default: "MM".
          '';

          DayFormat = settingOpt lib.types.str ''
            .NET datetime format string used for the day segment of
            generated folder names. Constructor default: "dd".
          '';

          DateFormatWithMonth = settingOpt lib.types.str ''
            .NET datetime format string for year+month folder names
            (e.g. when grouping photos by month). Constructor default:
            "yyyy.MM".
          '';

          DateFormatWithDay = settingOpt lib.types.str ''
            .NET datetime format string for year+month+day folder names
            (e.g. when grouping photos by day). Constructor default:
            "yyyy.MM.dd".
          '';

          DateTimeFormatWithMinutes = settingOpt lib.types.str ''
            .NET datetime format string including hours and minutes,
            used when minute-granular timestamps are needed in folder
            or file names. Constructor default: "yyyy.MM.dd_HH.mm".
          '';

          DateTimeFormatWithSeconds = settingOpt lib.types.str ''
            .NET datetime format string including hours, minutes, and
            seconds, used when second-granular timestamps are needed.
            Constructor default: "yyyy.MM.dd_HH.mm.ss".
          '';

          AddressSeparator = settingOpt lib.types.str ''
            Separator joining reverse-geocoded address components in
            generated folder names (country, city, district, ...).
            Constructor default: "-".
          '';

          FolderAppendSeparator = settingOpt lib.types.str ''
            Separator placed between a folder's base name and any
            appended content (e.g. coordinates suffixed onto an
            address-named folder). Constructor default: "-".
          '';

          DayRangeSeparator = settingOpt lib.types.str ''
            Separator between start and end dates when photos are
            organized by a contiguous day range. Constructor default:
            "-".
          '';

          SameNameNumberSeparator = settingOpt lib.types.str ''
            Separator between a filename and its disambiguating
            sequence number when multiple photos would otherwise land
            at the same path. Constructor default: "-".
          '';

          ArchivePhotoTakenDateHashSeparator = settingOpt lib.types.str ''
            Separator between the taken-date prefix and content-hash
            suffix in archive paths produced by the `archive` verb.
            Constructor default: "-". The bundled `appsettings.json`
            ships this with a misspelled key (`...Default`), so the
            JSON layer never overrides it even when loaded.
          '';

          PhotoFormatInvalidFolderName = settingOpt lib.types.str ''
            Folder name used to collect files whose format photo-cli
            cannot parse as a photo. Constructor default:
            "invalid-photo-format".
          '';

          NoPhotoTakenDateFolderName = settingOpt lib.types.str ''
            Folder name used to collect photos that have no EXIF
            taken-date metadata. Constructor default:
            "no-photo-taken-date".
          '';

          NoAddressFolderName = settingOpt lib.types.str ''
            Folder name used to collect photos that could not be
            reverse-geocoded to an address. Constructor default:
            "no-address".
          '';

          NoAddressAndPhotoTakenDateFolderName = settingOpt lib.types.str ''
            Folder name used to collect photos that lack both an
            address and a taken date. Constructor default:
            "no-address-and-no-photo-taken-date".
          '';

          CsvReportFileName = settingOpt lib.types.str ''
            Filename of the CSV report written by the
            organize/copy/archive verbs. Constructor default:
            "photo-cli-report.csv".
          '';

          DryRunCsvReportFileName = settingOpt lib.types.str ''
            Filename of the CSV report written when those verbs run
            in `--dry-run` mode. Constructor default:
            "photo-cli-dry-run.csv".
          '';

          ConnectionLimit = settingOpt lib.types.ints.u8 ''
            Maximum concurrent HTTP connections. Sets
            `ServicePointManager.DefaultConnectionLimit` process-wide
            and also caps the reverse-geocoder fetch semaphore. .NET
            `byte`, so 0-255. Constructor default: 4.
          '';

          CoordinatePrecision = settingOpt lib.types.ints.u8 ''
            Decimal places retained for latitude/longitude when
            coordinates are appended to folder names. .NET `byte`,
            so 0-255. Constructor default: 4.
          '';

          SupportedExtensions = settingOpt (lib.types.listOf lib.types.str) ''
            File extensions (lowercase, without leading dot) that
            photo-cli treats as photos. Setting `null` (the default)
            falls back to the constructor default
            `[ "jpg" "jpeg" "heic" "png" ]`; pass an explicit list to
            override. An empty list `[ ]` flattens to no env vars and
            therefore also falls back to the constructor default --
            it is not a way to disable photo recognition.
          '';

          ExpectedDayRange = settingOpt lib.types.ints.s16 ''
            Expected maximum span in days for a single grouped folder
            of photos. The copy/archive verbs warn (or stop, depending
            on the verb's `--no-taken-date` action) when a group
            covers more than this many days. .NET `short?`, so
            -32768..32767. Constructor default: null (no expectation).
          '';

          CompanionExtensions = settingOpt (lib.types.listOf lib.types.str) ''
            File extensions (lowercase, without leading dot) that are
            organized alongside photos -- e.g. video files captured
            on the same camera. Setting `null` (the default) falls
            back to the constructor default `[ "mov" ]`; an empty
            list also falls back, by the same mechanism described on
            `SupportedExtensions`.
          '';

          LogCategoryNameOutput = settingOpt lib.types.bool ''
            Whether to prepend each log message with its
            `Microsoft.Extensions.Logging` category name.
            Constructor default: false.
          '';

          LogLevel = lib.mkOption {
            type = lib.types.attrsOf logLevelEnum;
            default = { };
            example = {
              Default = "Warning";
              PhotoCli = "Information";
            };
            description = ''
              Per-category log levels, matching the
              `Microsoft.Extensions.Logging`
              `Dictionary<string, string>` shape. Keys are logger
              category names (e.g. "Default", "PhotoCli", "Microsoft",
              "System.Net.Http.HttpClient"); values are one of Trace,
              Debug, Information, Warning, Error, Critical, None.
              Empty by default; only categories you set here are
              overridden, the rest fall through to the constructor
              defaults (`Default=Error`, `PhotoCli=Warning`, plus
              `Polly`, `Microsoft`, `System.Net.Http.HttpClient`, and
              `PhotoCli.Services.Implementations.ReverseGeocodes` all
              at `Warning`).
            '';
          };

          MacOsCommand = settingOpt lib.types.str ''
            Shell command invoked by the `info` verb on macOS to
            display a photo. Constructor default: "open".
          '';

          MacOsArgumentPrefix = settingOpt lib.types.str ''
            Arguments prepended to `MacOsCommand` when displaying a
            photo on macOS (e.g. `-a Preview` to force the Preview
            app). Constructor default: "-a Preview".
          '';
        };
      };
    };

    commandDefaults = lib.mapAttrs (
      verb: spec:
      lib.mkOption {
        default = { };
        description = ''
          Default flag values for `photo-cli ${verb}`. Each option
          corresponds to a `[Option]` on `${verb}Options`
          (CommandLineParser 2.x in photo-cli). Unset options (`null`)
          emit nothing. The wrapper splices each non-null value in
          after the verb only when the user did not already supply
          that flag (in `--long`, `--long=val`, or `-x val` form), so
          shell-supplied flags always win and CommandLineParser's
          `RepeatedOptionInCommandLineError` cannot fire.

          Not modeled here on purpose:
          - Run-specific paths/IDs (`--input`, `--output`,
            `--album-name`, `--update-album`). These vary per run, so
            "default" makes no sense -- pass them on each invocation.
          - Flags duplicated by `ToolOptionsRaw` env vars. Set those
            via `dotfiles.photo-cli.settings` (e.g.
            `settings.ExpectedDayRange`) so each knob has one binding
            path.
          - API-key flags. Provide values via
            `dotfiles.photo-cli.geocoder.*` and sops.

          Limitations:
          - Bundled short flags like `-do` (meaning `-d -o`) bypass
            the duplicate-flag detection. Don't combine a bundled
            short with a Nix-declared default for the same flag --
            pass each short separately or use the long form.
          - List-typed values may not contain whitespace. photo-cli's
            `IEnumerable<T>` options are space-separated on the
            command line, so an element with embedded spaces becomes
            two list items.
        '';
        type = lib.types.submodule {
          options = lib.mapAttrs (_: meta: settingOpt meta.type meta.description) spec;
        };
      }
    ) verbSpecs;

    geocoder = {
      bigDataCloud = {
        enable = lib.mkEnableOption "BigDataCloud reverse geocoder";
        secretName = lib.mkOption {
          type = lib.types.str;
          default = "photo_cli_bigdatacloud_api_key";
          description = "sops secret name holding the BigDataCloud API key.";
        };
      };
      googleMaps = {
        enable = lib.mkEnableOption "Google Maps reverse geocoder";
        secretName = lib.mkOption {
          type = lib.types.str;
          default = "photo_cli_google_maps_api_key";
          description = "sops secret name holding the Google Maps API key.";
        };
      };
      locationIq = {
        enable = lib.mkEnableOption "LocationIq reverse geocoder";
        secretName = lib.mkOption {
          type = lib.types.str;
          default = "photo_cli_locationiq_api_key";
          description = "sops secret name holding the LocationIq API key.";
        };
      };
    };
  };

  config = {
    # Match the file-organizer naming policy
    # (configs/claude/skills/file-organizer/references/naming.md):
    # `-` separates words within a field, `_` separates fields,
    # periods are reserved for semantic compound extensions.
    dotfiles.photo-cli.settings = {
      DateFormatWithMonth = lib.mkDefault "yyyy-MM";
      DateFormatWithDay = lib.mkDefault "yyyy-MM-dd";
      DateTimeFormatWithMinutes = lib.mkDefault "yyyy-MM-dd_HH-mm";
      DateTimeFormatWithSeconds = lib.mkDefault "yyyy-MM-dd_HH-mm-ss";
    };

    home.packages = [ photoCliWrapped ];

    # Declare geocoder secrets only when the matching geocoder is enabled.
    # sops-install-secrets validates that every declared key exists in the
    # encrypted YAML at build time, so unconditional declarations would
    # break the build on hosts that haven't populated the values yet.
    sops.secrets = lib.mkIf sopsEnabled (
      lib.mkMerge [
        (lib.mkIf cfg.geocoder.bigDataCloud.enable {
          ${cfg.geocoder.bigDataCloud.secretName}.key = "PHOTO_CLI_BIGDATACLOUD_API_KEY";
        })
        (lib.mkIf cfg.geocoder.googleMaps.enable {
          ${cfg.geocoder.googleMaps.secretName}.key = "PHOTO_CLI_GOOGLE_MAPS_API_KEY";
        })
        (lib.mkIf cfg.geocoder.locationIq.enable {
          ${cfg.geocoder.locationIq.secretName}.key = "PHOTO_CLI_LOCATIONIQ_API_KEY";
        })
      ]
    );
  };
}
