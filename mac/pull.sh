#!/bin/bash

## Pull dotfiles from the system
##
rm -rf .config/*
cp -r ~/.config/* .config/
