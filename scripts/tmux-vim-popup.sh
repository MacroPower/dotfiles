#!/usr/bin/env bash
# Open a file path in vim inside a tmux popup.
# Called by tmux-thumbs @thumbs-upcase-command with matched text as $1.

input="${1?Usage: tmux-vim-popup <path>}"

# Expand leading tilde
input="${input/#\~/$HOME}"

# Split file:line if present
file="${input%%:*}"
line="${input#*:}"
if [ "$line" = "$file" ] || ! [[ $line =~ ^[0-9]+$ ]]; then
  line=1
fi

# Reject inputs that don't look like paths
if [[ $file != */* ]] && [[ $file != .* ]]; then
  echo "Not a file: $input"
  exit 1
fi

# Resolve relative paths against the active pane's working directory
if [[ $file != /* ]]; then
  dir="$(tmux display-message -p '#{pane_current_path}')"
  file="$dir/$file"
fi

# Normalize
file="$(realpath -m "$file")"

tmux-popup-run -T " vim " -w 90% -h 90% -d "$(dirname "$file")" vim +"$line" "$file"
