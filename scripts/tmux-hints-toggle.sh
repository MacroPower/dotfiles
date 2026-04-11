pane_id=$(tmux show-window-option -v @hints_pane 2>/dev/null || true)

if [ -n "$pane_id" ] && tmux list-panes -F '#{pane_id}' | grep -qF "$pane_id"; then
  tmux kill-pane -t "$pane_id"
  tmux set-window-option -u @hints_pane
else
  target=$(tmux display-message -p '#{pane_id}')
  tmux split-window -fh -l 30 tmux-hints
  hints_pane=$(tmux display-message -p '#{pane_id}')
  tmux set-window-option @hints_pane "$hints_pane"
  tmux select-pane -t "$target"
fi
