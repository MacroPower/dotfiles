#!/opt/homebrew/bin/fish

brew bundle install --cleanup
brew cu -y
fisher update
k krew update
k krew upgrade
