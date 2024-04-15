#!/opt/homebrew/bin/fish

brew bundle install --cleanup
brew cu -a
fisher update
k krew update
k krew upgrade

# Completions
curl https://raw.githubusercontent.com/go-task/task/main/completion/fish/task.fish > .config/fish/completions/task.fish
kyverno completion fish > .config/fish/completions/kyverno.fish
cilium completion fish > .config/fish/completions/cilium.fish
