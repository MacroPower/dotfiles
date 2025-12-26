#!/opt/homebrew/bin/fish

brew bundle install --cleanup
brew cu -y


fisher update
krewfile --file=.krewfile --command="kubectl krew" --upgrade
