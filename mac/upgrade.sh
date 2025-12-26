#!/opt/homebrew/bin/fish

brew bundle install --cleanup
brew cu -y

devbox version update

fisher update
krewfile --file=.krewfile --command="kubectl krew" --upgrade
