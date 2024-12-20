eval "$(/opt/homebrew/bin/brew shellenv)"
zoxide init fish | source
pyenv init - | source

# set:
# -g
# Sets a globally-scoped variable.
# Global variables are available to all functions running in the same shell.
# They can be modified or erased.
# -x
# Causes the specified shell variable to be exported to child processes
# (making it an "environment variable").

set -gx XDG_CONFIG_HOME "$HOME/.config"
set -gx FISH_AI_PYTHON_VERSION 3.12
set -gx devbox_no_prompt true

fish_add_path "$HOME/go/bin"
fish_add_path "$HOME/.krew/bin"

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
    neofetch --disable packages
end
