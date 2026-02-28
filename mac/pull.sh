#!/bin/bash

## Pull dotfiles from the system
##
rsync -a --delete ~/.config/ .config/

rsync -a --delete ~/.claude/agents/ .claude/agents/
rsync -a --delete ~/.claude/skills/ .claude/skills/
rsync -a --delete ~/.claude/settings.json .claude/settings.json
