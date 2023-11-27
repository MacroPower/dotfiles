# Mac

## Setup

### Bootstrap

```bash
## Create SSH key
##
ssh-keygen -t ed25519 -C "<email>"

## Install Xcode Command Line Tools
##
xcode-select --install

## Install Brew
##
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"

## Install packages via Brew
##
## Note: Launch VSCode, command+shift+p
## Run `Shell Command: Install 'code' command in PATH`
##
brew bundle install

## Set fish as the default shell
##
echo "/opt/homebrew/bin/fish" | sudo tee -a /etc/shells
chsh -s /opt/homebrew/bin/fish

## Push packages and dotfiles from this repo to the system
## 
## Note: This will set configuration on the machine to be equal to this repo.
## If there is any drift, it will result in packages and settings being removed.
##
./push.sh
```

### Tools

```bash
# Tailscale
firefox https://pkgs.tailscale.com/stable/#macos

# Krew
firefox https://krew.sigs.k8s.io/docs/user-guide/setup/install/
kubectl krew index add netshoot https://github.com/nilic/kubectl-netshoot.git
kubectl krew install netshoot/netshoot  # https://github.com/nilic/kubectl-netshoot
kubectl krew install sniff              # ksniff
kubectl krew install gadget             # inspektor gadget
kubectl krew install tree               # https://github.com/ahmetb/kubectl-tree
kubectl krew install access-matrix      # https://github.com/corneliusweig/rakkess

# Terraform
tfenv install 1.6.3 && tfenv use 1.6.3

# Python
pyenv init
pyenv install 3.8
pyenv install 3.7
pyenv install 3.6
pyenv install 2.7
pyenv local 2.7.18 3.6.15 3.7.16 3.8.16
```

### System Settings

- Keyboard -> Show Launchpad -> cmd+enter
- Keyboard -> Ctrl->Cmd, Cmd->Ctrl
- Sound -> Show Always
- General -> Color -> Purple
- General -> Default Web Browser -> Firefox
- Battery -> Power Adapter -> Disable Sleep
- Mouse -> Disable Natural Scrolling

### Node Exporter

For desktop/server monitoring

```bash
https://github.com/prometheus/node_exporter/blob/master/examples/launchctl/README.md

<string>/opt/homebrew/bin/node_exporter $(&lt; /opt/homebrew/etc/node_exporter.args)</string>

echo -- '--web.listen-address=0.0.0.0:9100' | \
	sudo tee /opt/homebrew/etc/node_exporter.args
```

## Upgrades

```sh
brew update
brew upgrade
fisher update
k krew update
k krew upgrade
```
