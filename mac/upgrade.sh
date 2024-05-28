#!/opt/homebrew/bin/fish

brew bundle install --cleanup
brew cu -a
fisher update
k krew update
k krew upgrade
