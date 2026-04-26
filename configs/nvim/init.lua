-- Options (only non-defaults; see :help nvim-defaults)
vim.opt.expandtab = true
vim.opt.tabstop = 2
vim.opt.shiftwidth = 2
vim.opt.softtabstop = 2
vim.opt.smartindent = true
vim.opt.number = true
vim.opt.relativenumber = true
vim.opt.mouse = "a" -- nvim default is "nvi"; "a" adds command-line mode
vim.opt.ignorecase = true
vim.opt.smartcase = true
vim.opt.scrolloff = 8
vim.opt.cursorline = true
vim.opt.wildmode = "longest:full,full"
-- Headless Linux (SSH into terrarium, container hosts) has no X/Wayland
-- clipboard tool. Force OSC 52 so yanks ride the same chain as the pbcopy
-- shim: Neovim -> inner tmux (set-clipboard on) -> outer tmux -> Ghostty.
-- macOS/X/Wayland keep Neovim's auto-detected provider (pbcopy / wl-copy /
-- xclip), which already talks to a real clipboard.
if vim.fn.has("mac") == 0 and vim.env.DISPLAY == nil and vim.env.WAYLAND_DISPLAY == nil then
  local osc52 = require("vim.ui.clipboard.osc52")
  vim.g.clipboard = {
    name = "OSC 52",
    copy = { ["+"] = osc52.copy("+"), ["*"] = osc52.copy("*") },
    paste = { ["+"] = osc52.paste("+"), ["*"] = osc52.paste("*") },
  }
end
vim.opt.clipboard = "unnamedplus"
vim.opt.updatetime = 100
vim.opt.termguicolors = true -- explicit; nvim auto-enables based on $COLORTERM
vim.opt.timeoutlen = 500
vim.opt.signcolumn = "yes" -- pin so gitsigns/diagnostics don't shift the buffer
vim.opt.splitright = true
vim.opt.splitbelow = true
vim.opt.inccommand = "split" -- live preview of :s in a split
vim.opt.confirm = true -- prompt instead of error on :q with unsaved changes
vim.opt.list = true
vim.opt.listchars = { tab = "» ", trail = "·", nbsp = "␣", extends = "›", precedes = "‹" }

-- Folding (nvim-ufo): start unfolded, show a 1-col fold gutter
vim.opt.foldcolumn = "1"
vim.opt.foldlevel = 99
vim.opt.foldlevelstart = 99
vim.opt.foldenable = true

-- Leader (must be set before plugin keymaps load)
vim.g.mapleader = " "
vim.g.maplocalleader = " "

-- Treesitter inspection (nvim core commands)
vim.keymap.set("n", "<leader>ti", "<cmd>Inspect<cr>", { desc = "inspect highlight" })
vim.keymap.set("n", "<leader>tt", "<cmd>InspectTree<cr>", { desc = "inspect tree" })

-- Persistent undo
local undodir = vim.fn.expand("~/.local/state/nvim/undo")
vim.fn.mkdir(undodir, "p")
vim.opt.undofile = true
vim.opt.undodir = undodir

-- Spell check for prose filetypes
vim.api.nvim_create_autocmd("FileType", {
  pattern = { "markdown", "gitcommit" },
  callback = function()
    vim.opt_local.spell = true
    vim.opt_local.spelllang = "en_us"
  end,
})
