#!/bin/bash

## Sync packages with the Brewfile
##
brew bundle install --cleanup

## Add dotfiles
##
rm -rf ~/.config/*
cp -r .config/* ~/.config

## Link dotfiles to the central .config directory where needed
##
rm -f ~/.gitconfig ~/.vimrc ~/Library/Application\ Support/Code/User/settings.json
ln -s ~/.config/vscode/settings.json ~/Library/Application\ Support/Code/User/settings.json
ln -s ~/.config/vim/.vimrc ~/.vimrc
ln -s ~/.config/git/.gitconfig ~/.gitconfig
