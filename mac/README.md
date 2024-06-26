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
kubectl krew install blame              # https://github.com/knight42/kubectl-blame
kubectl krew install cyclonus           # https://github.com/mattfenwick/kubectl-cyclonus
kubectl krew install get-all            # https://github.com/corneliusweig/ketall
kubectl krew install graph              # https://github.com/steveteuber/kubectl-graph
kubectl krew install stern              # https://github.com/stern/stern

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
- Sound -> Show Always
- General -> Color -> Purple
- General -> Default Web Browser -> Firefox
- Battery -> Power Adapter -> Disable Sleep
- Mouse -> Disable Natural Scrolling

```sh
displayplacer "id:s4294967295 res:2560x1440 hz:144 color_depth:7 enabled:true scaling:off origin:(0,0) degree:0"
displayplacer "id:s1686152384 res:2560x1440 hz:144 color_depth:8 enabled:true scaling:off origin:(-2560,0) degree:0"
```

### Node Exporter

For desktop/server monitoring, node_exporter is installed. Complete setup:

```bash
curl https://raw.githubusercontent.com/prometheus/node_exporter/master/examples/launchctl/io.prometheus.node_exporter.plist | \
sed 's|/usr/local/bin/node_exporter|/opt/homebrew/bin/node_exporter|g' | \
sed 's|/usr/local/etc/node_exporter.args|/opt/homebrew/etc/node_exporter.args|g' \
> /Library/LaunchDaemons/io.prometheus.node_exporter.plist

echo -- '--web.listen-address=0.0.0.0:9100' | \
	sudo tee /opt/homebrew/etc/node_exporter.args
```

Note: Modified from [the node_exporter docs](https://github.com/prometheus/node_exporter/blob/master/examples/launchctl/README.md).

## Upgrades

```sh
./upgrade.sh
```
