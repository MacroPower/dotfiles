# hex color -> ANSI 24-bit foreground escape
ansi_fg() {
  printf '\033[38;2;%d;%d;%dm' "0x${1:0:2}" "0x${1:2:2}" "0x${1:4:2}"
}
ansi_bold_fg() {
  printf '\033[1;38;2;%d;%d;%dm' "0x${1:0:2}" "0x${1:2:2}" "0x${1:4:2}"
}

render_hints() {
  local file="$1"
  local first=true stripped
  # \033[K = erase to end of line (clears stale text from previous render)
  local reset=$'\033[0m\033[K'
  local eol=$'\033[K'
  local accent_esc active_esc key_esc dim_esc
  accent_esc=$(ansi_bold_fg "$color_accent")
  active_esc=$(ansi_bold_fg "$color_active")
  key_esc=$(ansi_fg "$color_key")
  dim_esc=$(ansi_fg "$color_dim")

  while IFS= read -r line || [[ -n $line ]]; do
    stripped="${line#"${line%%[![:space:]]*}"}"

    if [[ -z $line ]]; then
      printf '%s\n' "$eol"
    elif $first; then
      printf '%s%s%s\n' "$accent_esc" "$line" "$reset"
      first=false
    elif [[ $stripped =~ ^[A-Z]{2,} ]] && ! [[ $stripped =~ [[:space:]]{4,} ]]; then
      printf '%s%s%s\n' "$active_esc" "$line" "$reset"
    else
      first=false
      local indent="${line%%[![:space:]]*}"
      local rest="${line:${#indent}}"
      local key_re='^([^ ]+) '
      if [[ $rest =~ $key_re ]]; then
        local key_part="${BASH_REMATCH[1]}"
        local placeholder=$'\x01'
        local styled="${key_part//\\\//$placeholder}"
        styled="${styled//\//${dim_esc}/${key_esc}}"
        styled="${styled//$placeholder//}"
        styled="${styled//·/${dim_esc}·${key_esc}}"
        printf '%s%s%s%s%s%s\n' "$indent" "$key_esc" "$styled" "$dim_esc" "${rest:${#key_part}}" "$reset"
      else
        printf '%s%s%s\n' "$dim_esc" "$line" "$reset"
      fi
    fi
  done <"$file"
}

self="$TMUX_PANE"
hints_dir="$HOME/.config/hints"
last_cmd=""
last_width=""
printf '\033[?7l' # disable line wrapping -- truncate at pane edge
clear

trap 'printf "\033[?7h"' EXIT

while true; do
  # Find the active pane's command in this window (excluding the hints pane)
  pane_info=$(tmux list-panes -F '#{pane_active} #{pane_id} #{pane_current_command}' 2>/dev/null) || break
  cmd=""
  while read -r active id pcmd; do
    if [ "$active" = "1" ] && [ "$id" != "$self" ]; then
      cmd="$pcmd"
      break
    fi
  done <<<"$pane_info"

  # If hints pane is focused, keep showing previous hints
  if [ -z "$cmd" ]; then
    sleep 0.5
    continue
  fi

  # Normalize: strip leading dot and trailing -wrapped (Nix wrappers)
  cmd="${cmd#.}"
  cmd="${cmd%-wrapped}"

  # Command aliases
  case "$cmd" in
  nvim) cmd="vim" ;;
  esac

  # Re-render on command change or pane resize
  cur_width=$(tput cols)
  if [ "$cmd" != "$last_cmd" ] || [ "$cur_width" != "$last_width" ]; then
    hint_file="$hints_dir/$cmd.txt"
    [ -f "$hint_file" ] || hint_file="$hints_dir/tmux.txt"

    # Render to variable, then paint in place to avoid flash
    output=$(render_hints "$hint_file")
    tput home
    printf '%s\n' "$output"
    tput ed
    last_cmd="$cmd"
    last_width="$cur_width"
  fi

  sleep 0.5
done
