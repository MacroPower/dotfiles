eval "$(/opt/homebrew/bin/brew shellenv)"
zoxide init fish | source

# set:
# -g
# Sets a globally-scoped variable.
# Global variables are available to all functions running in the same shell.
# They can be modified or erased.
# -x
# Causes the specified shell variable to be exported to child processes
# (making it an "environment variable").

set -gx XDG_CONFIG_HOME "$HOME/.config"
set -gx XDG_DATA_DIRS "/opt/homebrew/share"
set -gx devbox_no_prompt true
set -gx DYLD_LIBRARY_PATH "/opt/homebrew/opt/openssl/lib:$DYLD_LIBRARY_PATH"
set -gx EDITOR "vim"

set --global fish_key_bindings fish_default_key_bindings

set --global fish_color_autosuggestion 555 brblack
set --global fish_color_cancel -r
set --global fish_color_command blue
set --global fish_color_comment red
set --global fish_color_cwd green
set --global fish_color_cwd_root red
set --global fish_color_end green
set --global fish_color_error brred
set --global fish_color_escape brcyan
set --global fish_color_history_current --bold
set --global fish_color_host normal
set --global fish_color_host_remote yellow
set --global fish_color_normal normal
set --global fish_color_operator brcyan
set --global fish_color_param cyan
set --global fish_color_quote yellow
set --global fish_color_redirection cyan --bold
set --global fish_color_search_match white --background=brblack
set --global fish_color_selection white --bold --background=brblack
set --global fish_color_status red
set --global fish_color_user brgreen
set --global fish_color_valid_path --underline
set --global fish_pager_color_completion normal
set --global fish_pager_color_description B3A06D yellow -i
set --global fish_pager_color_prefix normal --bold --underline
set --global fish_pager_color_progress brwhite --background=cyan
set --global fish_pager_color_selected_background -r

fish_add_path "$HOME/go/bin"
fish_add_path "$HOME/.npm-packages/bin"
fish_add_path "$HOME/.krew/bin"
fish_add_path "$HOME/.local/bin" # uv

if status is-interactive
    alias ls=eza
    alias cat=bat
    alias cd=z
    alias find=fd
    alias top=btm
    alias watch='viddy'
    alias w='viddy'
    alias traceroute='trip'
    alias kubectl='kubecolor'
end

alias sed=gsed
alias k=kubectl
alias wk="watch -n 1 kubectl"
alias kx=kubectx
alias kn=kubens

function fish_greeting
    # neofetch --disable packages
end

# Added by OrbStack: command-line tools and integration
# This won't be added again if you remove it.
source ~/.orbstack/shell/init2.fish 2>/dev/null || :
