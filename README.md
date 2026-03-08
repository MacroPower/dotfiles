# dotfiles

## NixOS

### Bootstrap

```sh
git clone https://github.com/MacroPower/dotfiles && cd dotfiles

home-manager switch --flake ".#$(whoami)@$(hostname -s)"

gh auth login
task secrets:init

task switch
```

### Upgrade

```bash
task update
task switch
```

## NixOS (Orb)

### Bootstrap

```bash
git clone https://github.com/MacroPower/dotfiles && cd dotfiles

task vm:create
```

### Upgrade

```bash
task update
task vm:switch
```

## Darwin

Declarative macOS system configuration using [nix-darwin](https://github.com/LnL7/nix-darwin) + [home-manager](https://github.com/nix-community/home-manager).

### Prerequisites

Install [Lix](https://lix.systems/) (or Nix) with flakes enabled.

### Bootstrap

```bash
# Create SSH key
ssh-keygen -t ed25519 -C "<email>"

# Install Xcode Command Line Tools
xcode-select --install

# Install Brew (still needed for GUI casks)
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"

git clone https://github.com/MacroPower/dotfiles && cd dotfiles

sudo nix run nix-darwin -- switch --flake ".#$SUDO_USER@$(hostname -s)"

gh auth login
task secrets:init

task ssh:upload-key # optional

sudo task switch
```

### System Settings

- General -> Default Web Browser -> Firefox

### Display Configuration

```sh
task displays
```

### Upgrade

```sh
task update
sudo task switch
```
