{
  pkgs,
  lib,
  config,
  ...
}:

let
  cfg = config.dotfiles.btrix;

  defaultsYaml = pkgs.writeText "btrix-defaults.yaml" (builtins.toJSON cfg.defaults);

  btrix = pkgs.writeShellApplication {
    name = "btrix";
    runtimeInputs = [
      pkgs.coreutils-full
      pkgs.curl
      pkgs.docker
      pkgs.gnugrep
      pkgs.py-wacz
      pkgs.slugify
    ];
    text = ''
      cmd=''${1:-}
      if [ -z "$cmd" ]; then
        cat <<'EOF'
      btrix - browsertrix-crawler docker wrapper

      Usage:
        btrix crawl [args...]                 Run a crawl with the defaults YAML applied.
        btrix qa [args...]                    Run a QA pass with the defaults YAML applied.
        btrix indexer [args...]               Run the indexer entrypoint (no defaults).
        btrix create-login-profile [args...]  Launch the profile creation UI.
        btrix page <url> [opts] [-- args...]  Capture a single page end-to-end.
        btrix site <url> [opts] [-- args...]  Crawl under a URL prefix end-to-end.

      Helper options (page, site):
        --slug <slug>    Override the slug derived from the page <title>.
        --profile        Mount ./profiles/profile.tar.gz into the crawler.
        --               Forward all remaining args to the crawler verbatim.
      EOF
        exit 0
      fi
      shift

      case "$cmd" in
        crawl|qa)
          exec docker run --rm \
            -v "${defaultsYaml}:/etc/btrix/defaults.yaml:ro" \
            -v "$PWD:/crawls/" \
            "${cfg.image}" \
            "$cmd" --config /etc/btrix/defaults.yaml "$@"
          ;;

        indexer)
          exec docker run --rm \
            -v "$PWD:/crawls/" \
            "${cfg.image}" \
            indexer "$@"
          ;;

        create-login-profile)
          # `docker run -it` fails when stdin is not a TTY (writeShellApplication
          # runs under `set -euo pipefail`), so drop `-i` when invoked from a
          # script. The control page on :9223 still keeps the session alive.
          if [ -t 0 ]; then
            it_flag="-it"
          else
            it_flag="-t"
          fi
          exec docker run --rm "$it_flag" \
            -p 6080:6080 -p 9223:9223 \
            -v "$PWD:/crawls/" \
            "${cfg.image}" \
            create-login-profile "$@"
          ;;

        page|site)
          url=""
          slug_input=""
          use_profile=0
          extra_args=()
          while [ $# -gt 0 ]; do
            case "$1" in
              --slug)
                if [ "$#" -lt 2 ]; then
                  echo "btrix $cmd: --slug requires a value" >&2
                  exit 2
                fi
                slug_input="$2"
                shift 2
                ;;
              --profile)
                use_profile=1
                shift
                ;;
              --)
                shift
                extra_args=( "$@" )
                break
                ;;
              --*)
                echo "btrix $cmd: unknown option: $1" >&2
                echo "Pass crawler-specific flags after '--'." >&2
                exit 2
                ;;
              *)
                if [ -z "$url" ]; then
                  url="$1"
                  shift
                else
                  echo "btrix $cmd: unexpected positional arg: $1" >&2
                  exit 2
                fi
                ;;
            esac
          done

          if [ -z "$url" ]; then
            echo "Usage: btrix $cmd <url> [--slug <slug>] [--profile] [-- <crawler args>]" >&2
            exit 2
          fi

          # Verbatim host (no www-stripping, no aliasing); matches the
          # ~/Documents/archives/<host>/ convention in the web-archive skill.
          rest=''${url#*://}
          host=''${rest%%/*}
          host=''${host%%:*}

          # Slug source preference: --slug > <title> > last URL path segment.
          # `|| true` swallows non-zero from curl/grep (offline, no <title>).
          title_or_arg="$slug_input"
          if [ -z "$title_or_arg" ]; then
            title_or_arg=$(
              curl -sL --max-time 15 "$url" 2>/dev/null \
                | grep -oiP '<title[^>]*>\K[^<]+' \
                | head -n1 \
                | tr -d '\n' \
                || true
            )
            if [ -z "$title_or_arg" ]; then
              path=''${rest#*/}
              if [ "$path" = "$rest" ]; then
                title_or_arg="$host"
              else
                path=''${path%/}
                title_or_arg=''${path##*/}
                [ -z "$title_or_arg" ] && title_or_arg="$host"
              fi
            fi
          fi
          slug="$(date +%F)-$(slugify "$title_or_arg")"

          base="${cfg.archivesDir}/$host"
          mkdir -p "$base"
          cd "$base"

          # `--profile` is the in-container path; the host file lives next to
          # $PWD because we mount $PWD as /crawls/.
          profile_args=()
          if [ "$use_profile" -eq 1 ]; then
            if [ ! -f "./profiles/profile.tar.gz" ]; then
              echo "btrix $cmd: ./profiles/profile.tar.gz not found." >&2
              echo "Run 'btrix create-login-profile --url <login-url>' first." >&2
              exit 1
            fi
            profile_args=( --profile /crawls/profiles/profile.tar.gz )
          fi

          if [ "$cmd" = "page" ]; then
            scope_args=( --scopeType page )
          else
            scope_args=( --scopeType prefix )
          fi

          docker run --rm \
            -v "${defaultsYaml}:/etc/btrix/defaults.yaml:ro" \
            -v "$PWD:/crawls/" \
            "${cfg.image}" \
            crawl --config /etc/btrix/defaults.yaml \
              --url "$url" \
              --collection "$slug" \
              "''${scope_args[@]}" \
              "''${profile_args[@]}" \
              "''${extra_args[@]}"

          mv "collections/$slug/$slug.wacz" "./$slug.wacz"
          rm -rf "collections/$slug"

          wacz validate "./$slug.wacz"

          printf '%s\n' "$PWD/$slug.wacz"
          ;;

        *)
          echo "btrix: unknown subcommand: $cmd" >&2
          echo "Run 'btrix' with no args for usage." >&2
          exit 2
          ;;
      esac
    '';
  };
in
{
  options.dotfiles.btrix = {
    enable = lib.mkEnableOption "btrix (browsertrix-crawler) docker wrapper";

    image = lib.mkOption {
      type = lib.types.str;
      default = "webrecorder/browsertrix-crawler:1.8.2";
      description = "Container image:tag for browsertrix-crawler. Pinned for reproducibility; override per-host as needed.";
    };

    archivesDir = lib.mkOption {
      type = lib.types.str;
      default = "${config.home.homeDirectory}/Documents/archives";
      description = "Root for the <archivesDir>/<host>/<date>-<slug>.wacz convention used by `btrix page` and `btrix site`.";
    };

    defaults = lib.mkOption {
      type = lib.types.attrsOf lib.types.anything;
      default = {
        generateWACZ = true;
        workers = 4;
        behaviors = [
          "autoscroll"
          "siteSpecific"
        ];
      };
      description = ''
        Rendered to YAML and injected as `--config <store-path>` into every
        `btrix crawl` and `btrix qa` invocation, positionally before the
        user's args so per-call CLI flags still win (yargs `.config()`
        semantics).

        `scopeType` is intentionally omitted -- `btrix page` and
        `btrix site` set it themselves so the two helpers cannot bleed
        into each other.

        Array-typed crawler options (e.g. `behaviors`,
        `src/util/argParser.ts:376-381`) must be Nix lists; a comma-joined
        string is silently mis-coerced by the YAML loader even though the
        CLI form accepts it.
      '';
    };

  };

  config = lib.mkIf cfg.enable {
    home.packages = [
      btrix
      pkgs.py-wacz
    ];
  };
}
