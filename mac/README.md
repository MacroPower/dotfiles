# Mac

## Setup

### Bootstrap

```bash
xcode-select --install
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
brew install --cask firefox

# Create SSH key
ssh-keygen -t ed25519 -C "<email>"
```

### Tools

```bash
# Utils
brew install neofetch   # Show system info
brew install dos2unix   # dos2unix for mac
brew install fd         # better find        https://github.com/sharkdp/fd
brew install bat        # better cat         https://github.com/sharkdp/bat
brew install fzf        # fuzzy finder       https://github.com/junegunn/fzf#usage
brew install zoxide     # better cd          https://github.com/ajeetdsouza/zoxide
brew install diskonaut  # tui disk navigator https://github.com/imsnif/diskonaut
brew install eza        # better ls          https://github.com/eza-community/eza
brew install bottom     # better top         https://github.com/ClementTsang/bottom
brew install git-delta  # better git diff    https://github.com/dandavison/delta
brew install arping     # arp ping           https://github.com/ThomasHabets/arping
brew install viddy      # better watch       https://github.com/sachaos/viddy
brew install --cask linearmouse
brew install --cask obsidian
brew install --cask keka
brew install --cask gpg-suite-no-mail
brew install --cask drawio

# Terminal
brew install --cask kitty
brew install fish
echo "/opt/homebrew/bin/fish" | sudo tee -a /etc/shells
chsh -s /opt/homebrew/bin/fish
brew tap homebrew/cask-fonts
brew install font-fira-code
brew install font-fira-code-nerd-font
brew install fisher
fisher install IlanCosman/tide@v5         # fish prompt    https://github.com/IlanCosman/tide
fisher install PatrickF1/fzf.fish         # fzf bindings   https://github.com/PatrickF1/fzf.fish
fisher install jorgebucaran/autopair.fish # char pairs     https://github.com/jorgebucaran/autopair.fish
fisher install oh-my-fish/plugin-jump     # project jump   https://github.com/oh-my-fish/plugin-pj

# Fun
brew install --cask spotify
brew install --cask discord
brew install --cask plex
brew install --cask vlc

# Dev
brew install go
brew install kubectl
brew install kustomize
brew install helm
brew install kubectx
brew install go-jsonnet
brew install jsonnet-bundler
brew install --cask db-browser-for-sqlite
brew install --cask openlens # add `@alebcay/openlens-node-pod-menu**`
brew install --cask visual-studio-code # install in path
brew install --cask fork
brew install --cask orbstack
brew install --cask wireshark
brew install wireshark
brew install tfenv
tfenv install 1.6.3 && tfenv use 1.6.3

firefox https://krew.sigs.k8s.io/docs/user-guide/setup/install/
kubectl krew index add netshoot https://github.com/nilic/kubectl-netshoot.git
kubectl krew install netshoot/netshoot  # https://github.com/nilic/kubectl-netshoot
kubectl krew install sniff              # ksniff
kubectl krew install gadget             # inspektor gadget
kubectl krew install tree               # https://github.com/ahmetb/kubectl-tree
kubectl krew install access-matrix      # https://github.com/corneliusweig/rakkess

code --install-extension PKief.material-icon-theme
code --install-extension zhuangtongfa.material-theme
code --install-extension eamodio.gitlens
code --install-extension EditorConfig.EditorConfig
code --install-extension esbenp.prettier-vscode
code --install-extension hashicorp.terraform
code --install-extension kokakiwi.vscode-just
code --install-extension golang.go
code --install-extension WakaTime.vscode-wakatime
code --install-extension GitHub.copilot
code --install-extension GitHub.copilot-chat
code --install-extension stkb.Rewrap
code --install-extension Grafana.vscode-jsonnet
code --install-extension ms-kubernetes-tools.vscode-kubernetes-tools
```

### System Settings

- Keyboard -> Show Launchpad -> cmd+enter
- Keyboard -> Ctrl->Cmd, Cmd->Ctrl
- Sound -> Show Always
- General -> Color -> Purple
- General -> Default Web Browser -> Firefox
- Battery -> Power Adapter -> Disable Sleep
- Mouse -> Disable Natural Scrolling

### Python

```bash
brew install pyenv
pyenv init
pyenv install 3.8
pyenv install 3.7
pyenv install 3.6
pyenv install 2.7

pyenv local 2.7.18 3.6.15 3.7.16 3.8.16
```

### Node Exporter

For desktop/server monitoring

```bash
brew install node_exporter

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
