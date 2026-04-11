set sts=2    " softtabstop (Let backspace delete indent)
set ai       " autoindent (Indent at the same level of the previous line)
set si       " smart indent
set hls      " highlightsearch (Highlight search terms)
syntax on    " enable syntax highlighting

" Search
set incsearch

" UI
set scrolloff=8
set cursorline
set wildmenu
set wildmode=longest:full,full

" System integration
set clipboard=unnamedplus

" Ensure undo directory exists
if !isdirectory($HOME . "/.vim/undodir")
  call mkdir($HOME . "/.vim/undodir", "p", 0700)
endif

" Leader key
let mapleader = "\<Space>"
set timeoutlen=500

" fzf.vim keybindings
nnoremap <C-p> :Files<CR>
nnoremap <leader>f :Rg<CR>
nnoremap <leader>b :Buffers<CR>

" vim-gitgutter responsiveness
set updatetime=100

" Mouse support
set ttymouse=sgr
set balloonevalterm
" Styled and colored underline support
let &t_AU = "\e[58:5:%dm"
let &t_8u = "\e[58:2:%lu:%lu:%lum"
let &t_Us = "\e[4:2m"
let &t_Cs = "\e[4:3m"
let &t_ds = "\e[4:4m"
let &t_Ds = "\e[4:5m"
let &t_Ce = "\e[4:0m"
" Strikethrough
let &t_Ts = "\e[9m"
let &t_Te = "\e[29m"
" Truecolor support
let &t_8f = "\e[38:2:%lu:%lu:%lum"
let &t_8b = "\e[48:2:%lu:%lu:%lum"
let &t_RF = "\e]10;?\e\\"
let &t_RB = "\e]11;?\e\\"
" Bracketed paste
let &t_BE = "\e[?2004h"
let &t_BD = "\e[?2004l"
let &t_PS = "\e[200~"
let &t_PE = "\e[201~"
" Cursor control
let &t_RC = "\e[?12$p"
let &t_SH = "\e[%d q"
let &t_RS = "\eP$q q\e\\"
let &t_SI = "\e[5 q"
let &t_SR = "\e[3 q"
let &t_EI = "\e[1 q"
let &t_VS = "\e[?12l"
" Focus tracking
let &t_fe = "\e[?1004h"
let &t_fd = "\e[?1004l"
execute "set <FocusGained>=\<Esc>[I"
execute "set <FocusLost>=\<Esc>[O"
" Window title
let &t_ST = "\e[22;2t"
let &t_RT = "\e[23;2t"

" vim hardcodes background color erase even if the terminfo file does
" not contain bce. This causes incorrect background rendering when
" using a color theme with a background color in terminals such as
" kitty that do not support background color erase.
let &t_ut=''

" Inspect $TERM instad of t_Co as it works in neovim as well
if $COLORTERM == 'truecolor' || $COLORTERM == '24bit' || &term =~ '256color'
  " Enable true (24-bit) colors instead of (8-bit) 256 colors.
  if has('termguicolors')
    let &t_8f = "\<Esc>[38;2;%lu;%lu;%lum"
    let &t_8b = "\<Esc>[48;2;%lu;%lu;%lum"
    set termguicolors
  endif
endif

" Theme
colorscheme onedark
hi Normal guibg=#23272e ctermbg=NONE

" Airline
let g:airline_theme='onedark'
let g:airline_powerline_fonts = 1

" vim-which-key
let g:which_key_map = {}
let g:which_key_map.f = 'ripgrep search'
let g:which_key_map.b = 'buffers'
call which_key#register('<Space>', "g:which_key_map")
nnoremap <silent> <leader> :<c-u>WhichKey '<Space>'<CR>
vnoremap <silent> <leader> :<c-u>WhichKeyVisual '<Space>'<CR>
