{
  lib,
  symlinkJoin,
  writeShellApplication,
  git,
  uv,
  coreutils,
  gnupatch,
  curl,
  unzip,
  # User config: override these from the consuming home-manager module.
  # The sentinel default keeps `pkgs.comfyui` evaluable; the launcher
  # itself prints a clear "not configured" error if invoked at the
  # sentinel path.
  dataDir ? "/var/empty/comfyui-not-configured",
  port ? 8188,
}:

let
  # Pinned commits — bump deliberately:
  comfyuiRev = "64b8457f55cd7fb54ca7a956d9c73b505e903e0c"; # v0.20.1
  managerRev = "66108ccdbc8cfc9549e42190d114d86d20fbe142"; # 2026-04-27
  # NOTE: SUPIR is research / non-commercial use only (Fanghua-Yu/SUPIR
  # license; the kijai wrapper inherits this). Do not redistribute outputs
  # for commercial purposes.
  supirRev = "fe0d660f7c02556f6ae9affa3357d871358ebea7"; # 2026-03-15
  bopbtlRev = "62441ab213b3e8e6a392d1227b01d47729250db2"; # 2025-06-30
  ddcolorRev = "30d5b8e7666382a6a78404caee28dfa87f741037"; # 2024-01-18
  pythonVersion = "3.11";

  supirPatch = ./comfyui-supir-no-accelerate.patch;

  dataDirArg = lib.escapeShellArg dataDir;

  setupScript = ''
    # writeShellApplication sets errexit/nounset/pipefail; add the shopt
    # we still need for the REQ_FILES glob and the $STASH dotglob block.
    shopt -s nullglob
    DATA_DIR=${dataDirArg}
    REPO="$DATA_DIR/ComfyUI"
    PY="$DATA_DIR/.venv/bin/python"

    mkdir -p "$DATA_DIR"

    # Recover from a previous failed activation that left $REPO populated
    # but without a .git directory (e.g., pre-creating subdirs caused the
    # ComfyUI clone to refuse). Move the existing tree aside, clone fresh,
    # then merge the stashed content back over the new checkout. Any tracked
    # ComfyUI file wins over a same-named user file; user-only paths
    # (custom_nodes/*, models/*) are preserved.
    STASH=""
    if [ ! -d "$REPO/.git" ] && [ -d "$REPO" ]; then
      STASH="$DATA_DIR/.recovery-$$"
      mv "$REPO" "$STASH"
    fi

    SETUP_FAILED=0

    sync_repo() {
      local url="$1" dest="$2" rev="$3"
      if [ ! -d "$dest/.git" ]; then
        git clone --quiet "$url" "$dest" || {
          echo "comfyui setup: clone $url failed (offline?); skipping" >&2
          SETUP_FAILED=1
          return 0
        }
      fi
      if git -C "$dest" rev-parse --verify --quiet "$rev^{commit}" >/dev/null; then
        git -C "$dest" checkout --quiet --detach "$rev" || {
          echo "comfyui setup: checkout $rev in $dest failed (dirty tree?)" >&2
          SETUP_FAILED=1
        }
      else
        git -C "$dest" fetch --quiet origin || {
          echo "comfyui setup: fetch in $dest failed; using current checkout" >&2
          SETUP_FAILED=1
          return 0
        }
        git -C "$dest" checkout --quiet --detach "$rev" || {
          echo "comfyui setup: rev $rev unreachable in $dest; staying on HEAD" >&2
          SETUP_FAILED=1
        }
      fi
      # Always init submodules. No-op for repos without any (e.g. ComfyUI
      # core, Manager, SUPIR, DDColor); critical for BOPBTL which keeps
      # its Microsoft backend code in a submodule.
      git -C "$dest" submodule update --init --recursive --quiet || {
        echo "comfyui setup: submodule init in $dest failed" >&2
        SETUP_FAILED=1
      }
    }

    apply_supir_patch() {
      local supir_dir="$REPO/custom_nodes/ComfyUI-SUPIR"
      local patch_file=${supirPatch}
      # Skip if clone failed earlier or the target file isn't present.
      [ -d "$supir_dir" ] && [ -f "$supir_dir/nodes_v2.py" ] || return 0
      # patch -R --dry-run succeeds iff the patch is already applied
      # (reverse-applies cleanly) -- standard idempotency idiom.
      if patch -d "$supir_dir" -p1 -R --dry-run -s -i "$patch_file" >/dev/null 2>&1; then
        return 0
      fi
      # Keep the tree clean on a divergent rev: --no-backup-if-mismatch
      # suppresses .orig, -r - sends rejected hunks to stdout instead of
      # writing nodes_v2.py.rej next to the source.
      if ! patch -d "$supir_dir" -p1 -s --no-backup-if-mismatch -r - -i "$patch_file"; then
        echo "comfyui setup: SUPIR patch did not apply (upstream moved?); see $patch_file" >&2
        SETUP_FAILED=1
      fi
    }

    sync_repo https://github.com/comfyanonymous/ComfyUI            "$REPO" "${comfyuiRev}"

    # Restore stashed user content over the fresh ComfyUI checkout. If the
    # clone failed, the stash becomes the current tree again so the next run
    # can retry without losing data.
    if [ -n "$STASH" ] && [ -d "$STASH" ]; then
      if [ -d "$REPO/.git" ]; then
        shopt -s dotglob
        for entry in "$STASH"/*; do
          base=$(basename "$entry")
          if [ -d "$entry" ] && [ -d "$REPO/$base" ]; then
            for sub in "$entry"/*; do
              [ -e "$REPO/$base/$(basename "$sub")" ] || mv "$sub" "$REPO/$base/"
            done
            rmdir "$entry" 2>/dev/null || true
          elif [ ! -e "$REPO/$base" ]; then
            mv "$entry" "$REPO/"
          fi
        done
        shopt -u dotglob
        rm -rf "$STASH"
      else
        rmdir "$REPO" 2>/dev/null || true
        mv "$STASH" "$REPO"
      fi
    fi

    sync_repo https://github.com/ltdrdata/ComfyUI-Manager          "$REPO/custom_nodes/ComfyUI-Manager" "${managerRev}"
    sync_repo https://github.com/kijai/ComfyUI-SUPIR               "$REPO/custom_nodes/ComfyUI-SUPIR" "${supirRev}"
    apply_supir_patch
    sync_repo https://github.com/Haoming02/comfyui-old-photo-restoration "$REPO/custom_nodes/comfyui-old-photo-restoration" "${bopbtlRev}"
    sync_repo https://github.com/kijai/ComfyUI-DDColor                    "$REPO/custom_nodes/ComfyUI-DDColor" "${ddcolorRev}"

    if [ ! -f "$REPO/requirements.txt" ]; then
      echo "comfyui setup: $REPO/requirements.txt missing; aborting venv work" >&2
      exit 1
    fi

    if [ ! -x "$PY" ]; then
      uv venv --quiet --python ${pythonVersion} "$DATA_DIR/.venv"
    fi

    REQ_HASH_FILE="$DATA_DIR/.venv/.req-hash"
    REQ_FILES=( "$REPO/requirements.txt" "$REPO"/custom_nodes/*/requirements.txt )
    NEW_HASH=$(sha256sum "''${REQ_FILES[@]}" | sha256sum | cut -d' ' -f1)
    if [ ! -f "$REQ_HASH_FILE" ] || [ "$(cat "$REQ_HASH_FILE")" != "$NEW_HASH" ]; then
      INSTALL_FAILED=0
      for req in "''${REQ_FILES[@]}"; do
        uv pip install --python "$PY" -r "$req" || {
          echo "comfyui setup: $req install failed; will retry next switch" >&2
          INSTALL_FAILED=1
          SETUP_FAILED=1
        }
      done
      if [ "$INSTALL_FAILED" -eq 0 ]; then
        echo "$NEW_HASH" > "$REQ_HASH_FILE"
      fi
    fi

    [ "$SETUP_FAILED" -eq 0 ]
  '';

  comfyui-launcher = writeShellApplication {
    name = "comfyui";
    runtimeInputs = [ ];
    text = ''
      DATA_DIR=${dataDirArg}
      REPO="$DATA_DIR/ComfyUI"
      if [ ! -x "$DATA_DIR/.venv/bin/python" ] || [ ! -f "$REPO/main.py" ]; then
        echo "comfyui: ComfyUI not fully installed at $DATA_DIR." >&2
        echo "  run 'task switch' (online) or 'comfyui-update' to finish setup" >&2
        exit 1
      fi
      cd "$REPO"
      exec "$DATA_DIR/.venv/bin/python" main.py \
        --listen 127.0.0.1 --port ${toString port} "$@"
    '';
  };

  comfyui-update = writeShellApplication {
    name = "comfyui-update";
    runtimeInputs = [
      git
      uv
      coreutils
      gnupatch
    ];
    text = setupScript;
  };

  # SDXL backbone + SUPIR fp16 weights. The SUPIR repo is research /
  # non-commercial only; same caveat as the wrapper code itself.
  modelDownloads = [
    {
      url = "https://huggingface.co/stabilityai/stable-diffusion-xl-base-1.0/resolve/main/sd_xl_base_1.0.safetensors";
      dest = "models/checkpoints/sd_xl_base_1.0.safetensors";
      desc = "SDXL 1.0 base (~6.9 GB)";
    }
    {
      url = "https://huggingface.co/Kijai/SUPIR_pruned/resolve/main/SUPIR-v0F_fp16.safetensors";
      dest = "models/checkpoints/SUPIR-v0F_fp16.safetensors";
      desc = "SUPIR-v0F fp16 (faded photos, ~5 GB)";
    }
    {
      url = "https://huggingface.co/Kijai/SUPIR_pruned/resolve/main/SUPIR-v0Q_fp16.safetensors";
      dest = "models/checkpoints/SUPIR-v0Q_fp16.safetensors";
      desc = "SUPIR-v0Q fp16 (general quality, ~5 GB)";
    }
    {
      url = "https://github.com/xinntao/Real-ESRGAN/releases/download/v0.1.0/RealESRGAN_x4plus.pth";
      dest = "models/upscale_models/RealESRGAN_x4plus.pth";
      desc = "Real-ESRGAN x4plus (general 4x upscaler, ~64 MB)";
    }
  ];

  # ZIP archives that need to be extracted into specific paths inside the
  # ComfyUI tree. The `sentinel` is a file inside `extractTo` whose presence
  # means the archive is already unpacked — used to skip on rerun.
  modelArchives = [
    {
      url = "https://github.com/Haoming02/sd-webui-old-photo-restoration/releases/download/1.0/global_checkpoints.zip";
      extractTo = "custom_nodes/comfyui-old-photo-restoration/lib_bopb2l/Global";
      sentinel = "custom_nodes/comfyui-old-photo-restoration/lib_bopb2l/Global/checkpoints/detection/FT_Epoch_latest.pt";
      desc = "BOPBTL global restoration checkpoints (~700 MB)";
    }
    {
      url = "https://github.com/Haoming02/sd-webui-old-photo-restoration/releases/download/1.0/face_checkpoints.zip";
      extractTo = "custom_nodes/comfyui-old-photo-restoration/lib_bopb2l/Face_Enhancement";
      sentinel = "custom_nodes/comfyui-old-photo-restoration/lib_bopb2l/Face_Enhancement/checkpoints/Setting_9_epoch_100/latest_net_G.pth";
      desc = "BOPBTL face enhancement checkpoints (~500 MB)";
    }
    {
      url = "https://github.com/Haoming02/sd-webui-old-photo-restoration/releases/download/1.0/shape_predictor_68_face_landmarks.zip";
      extractTo = "custom_nodes/comfyui-old-photo-restoration/lib_bopb2l/Face_Detection";
      sentinel = "custom_nodes/comfyui-old-photo-restoration/lib_bopb2l/Face_Detection/shape_predictor_68_face_landmarks.dat";
      desc = "BOPBTL dlib face landmark predictor (~95 MB)";
    }
  ];

  comfyui-fetch-models = writeShellApplication {
    name = "comfyui-fetch-models";
    runtimeInputs = [
      curl
      coreutils
      unzip
    ];
    text = ''
      DATA_DIR=${dataDirArg}
      REPO="$DATA_DIR/ComfyUI"

      if [ ! -d "$REPO" ]; then
        echo "comfyui-fetch-models: $REPO missing; run 'comfyui-update' first" >&2
        exit 1
      fi

      fetch_one() {
        local url="$1" dest="$2" desc="$3"
        if [ -f "$dest" ]; then
          echo "[skip] $desc -> $dest"
          return 0
        fi
        echo "[get ] $desc"
        echo "       $url"
        mkdir -p "$(dirname "$dest")"
        if ! curl -L --fail --retry 3 --retry-delay 5 \
          --progress-bar -o "$dest.partial" "$url"; then
          rm -f "$dest.partial"
          echo "comfyui-fetch-models: download failed for $desc" >&2
          return 1
        fi
        mv "$dest.partial" "$dest"
        echo "[ok  ] $dest"
      }

      fetch_zip() {
        local url="$1" extract_to="$2" sentinel="$3" desc="$4"
        if [ -f "$sentinel" ]; then
          echo "[skip] $desc -> $sentinel"
          return 0
        fi
        echo "[get ] $desc"
        echo "       $url"
        mkdir -p "$extract_to"
        local tmpzip
        tmpzip=$(mktemp -t comfyui-fetch.XXXXXX.zip)
        if ! curl -L --fail --retry 3 --retry-delay 5 \
          --progress-bar -o "$tmpzip" "$url"; then
          rm -f "$tmpzip"
          echo "comfyui-fetch-models: download failed for $desc" >&2
          return 1
        fi
        if ! unzip -q -o "$tmpzip" -d "$extract_to"; then
          rm -f "$tmpzip"
          echo "comfyui-fetch-models: extract failed for $desc" >&2
          return 1
        fi
        rm -f "$tmpzip"
        if [ ! -f "$sentinel" ]; then
          echo "comfyui-fetch-models: sentinel $sentinel missing after extract" >&2
          return 1
        fi
        echo "[ok  ] $sentinel"
      }

      FAILED=0
      ${lib.concatMapStringsSep "\n      " (m: ''
        fetch_one ${lib.escapeShellArg m.url} "$REPO/${m.dest}" ${lib.escapeShellArg m.desc} || FAILED=1
      '') modelDownloads}
      ${lib.concatMapStringsSep "\n      " (a: ''
        fetch_zip ${lib.escapeShellArg a.url} "$REPO/${a.extractTo}" "$REPO/${a.sentinel}" ${lib.escapeShellArg a.desc} || FAILED=1
      '') modelArchives}

      if [ "$FAILED" -ne 0 ]; then
        echo "comfyui-fetch-models: one or more downloads failed; rerun to resume" >&2
        exit 1
      fi
      echo "comfyui-fetch-models: done. Restart comfyui to pick up new checkpoints."
    '';
  };
in
symlinkJoin {
  name = "comfyui";
  paths = [
    comfyui-launcher
    comfyui-update
    comfyui-fetch-models
  ];
  passthru = {
    inherit dataDir port;
  };
  meta = {
    description = "ComfyUI photo-restoration toolkit (launcher, updater, model fetcher)";
    homepage = "https://github.com/comfyanonymous/ComfyUI";
    license = lib.licenses.gpl3Only;
    mainProgram = "comfyui";
    platforms = lib.platforms.unix;
  };
}
