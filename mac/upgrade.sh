#!/opt/homebrew/bin/fish

brew bundle install --cleanup
brew cu -y

pyenv install --skip-existing 3.10
pyenv install --skip-existing 3.11
pyenv install --skip-existing 3.12
pyenv install --skip-existing 3.13
pyenv global 3.12

tfenv install latest
tfenv use latest

fisher update
krewfile --file=.krewfile --command="kubectl krew" --upgrade
