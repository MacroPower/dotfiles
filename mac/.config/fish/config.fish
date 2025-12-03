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

alias k=kubectl
alias wk="watch -n 1 kubectl"
alias kx=kubectx
alias kn=kubens

alias ssh="kitty +kitten ssh"
alias diff="kitty +kitten diff"

function fish_greeting
    # neofetch --disable packages
end

# Added by OrbStack: command-line tools and integration
# This won't be added again if you remove it.
source ~/.orbstack/shell/init2.fish 2>/dev/null || :
