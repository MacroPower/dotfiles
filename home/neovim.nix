{ config, pkgs, ... }:

let
  base00 = "#${config.lib.stylix.colors.base00}";
  inherit (import ../lib/nerdfonts.nix) icons;
in
{
  programs.neovim = {
    enable = true;
    viAlias = true;
    vimAlias = true;
    vimdiffAlias = true;

    extraPackages = with pkgs; [
      ripgrep
      fd
      cookie

      # LSP servers
      gopls
      nixd
      lua-language-server
      bash-language-server
      pyright
      yaml-language-server
      vscode-langservers-extracted # jsonls, html, css, eslint
      taplo
      marksman
      dockerfile-language-server
      terraform-ls

      # Formatters / linters used by conform.nvim and nvim-lint
      stylua
      nixfmt
      shfmt
      shellcheck
      statix
      deadnix
      yamllint
      prettierd
      opentofu
      ruff
      gotools
      gofumpt
    ];

    plugins = with pkgs.vimPlugins; [
      # Loaded first so MiniIcons.mock_nvim_web_devicons() registers
      # the package.preload shim before telescope requires it,
      # and so mini.notify provides vim.notify before noice loads.
      {
        plugin = mini-nvim;
        type = "lua";
        config = ''
          require("mini.pairs").setup({})
          require("mini.ai").setup({})
          require("mini.icons").setup({})
          MiniIcons.mock_nvim_web_devicons()
          require("mini.notify").setup({})
          require("mini.surround").setup({})
          require("mini.indentscope").setup({
            symbol = "│",
            options = { try_as_border = true },
            draw = { animation = require("mini.indentscope").gen_animation.none() },
          })
          require("mini.bracketed").setup({})
          require("mini.splitjoin").setup({})
          require("mini.files").setup({ windows = { preview = true } })
          require("mini.sessions").setup({ autoread = false, autowrite = true })

          local starter = require("mini.starter")
          starter.setup({
            evaluate_single = true,
            items = {
              starter.sections.recent_files(5, false),
              starter.sections.sessions(5, true),
              starter.sections.builtin_actions(),
            },
            content_hooks = {
              starter.gen_hook.adding_bullet(),
              starter.gen_hook.aligning("center", "center"),
            },
            header = function()
              local out = vim.fn.system({ "cookie", "-s", "-cow", "random" })
              return (out:gsub("%s+$", ""))
            end,
            footer = "",
          })

          require("mini.snippets").setup({
            snippets = {
              require("mini.snippets").gen_loader.from_lang(),
            },
          })

          vim.keymap.set("n", "-", function()
            MiniFiles.open(vim.api.nvim_buf_get_name(0))
          end, { desc = "open parent dir" })

          vim.keymap.set("n", "<leader>qs", function() MiniSessions.read() end,
            { desc = "restore session" })
          vim.keymap.set("n", "<leader>ql", function()
            MiniSessions.read(MiniSessions.get_latest())
          end, { desc = "restore last session" })

          require("mini.jump2d").setup({
            mappings = { start_jumping = "s" },
          })

          require("mini.diff").setup({})
          vim.keymap.set("n", "<leader>gn", "]h", { desc = "next hunk", remap = true })
          vim.keymap.set("n", "<leader>gp", "[h", { desc = "prev hunk", remap = true })
          local function hunk_at_cursor(action)
            local line = vim.api.nvim_win_get_cursor(0)[1]
            require("mini.diff").do_hunks(0, action, { line_start = line, line_end = line })
          end
          vim.keymap.set("n", "<leader>gS", function() hunk_at_cursor("apply") end,
            { desc = "stage hunk" })
          vim.keymap.set("n", "<leader>gR", function() hunk_at_cursor("reset") end,
            { desc = "reset hunk" })
          vim.keymap.set("n", "<leader>gP", function()
            require("mini.diff").toggle_overlay()
          end, { desc = "toggle hunk overlay" })

          require("mini.git").setup({})
          vim.keymap.set("n", "<leader>gs", "<cmd>Git<cr>",            { desc = "status" })
          vim.keymap.set("n", "<leader>gb", "<cmd>Git blame -- %<cr>", { desc = "blame" })

          vim.api.nvim_create_autocmd("FileType", {
            pattern = { "help", "lazy", "mason", "notify", "Trouble", "trouble", "minifiles", "starter", "TelescopePrompt" },
            callback = function() vim.b.miniindentscope_disable = true end,
          })
        '';
      }

      # Appearance
      {
        plugin = onedark-nvim;
        type = "lua";
        config = ''
          require("onedark").setup({
            style = "dark",
            colors = { bg0 = "${base00}" },
            highlights = {
              NormalFloat = { bg = "${base00}" },
              FloatBorder = { bg = "${base00}" },
            },
          })
          require("onedark").load()
        '';
      }
      {
        plugin = lualine-nvim;
        type = "lua";
        config = ''
          require("lualine").setup({
            options = { theme = "onedark", icons_enabled = true },
          })
        '';
      }

      # Navigation
      plenary-nvim
      telescope-fzf-native-nvim
      {
        plugin = telescope-nvim;
        type = "lua";
        config = ''
          local builtin = require("telescope.builtin")
          require("telescope").setup({})
          pcall(require("telescope").load_extension, "fzf")
          vim.keymap.set("n", "<leader>p", builtin.find_files, { desc = "files" })
          vim.keymap.set("n", "<leader>f", builtin.live_grep, { desc = "ripgrep" })
          vim.keymap.set("n", "<leader>b", builtin.buffers, { desc = "buffers" })
          vim.keymap.set("n", "<leader>r", builtin.resume, { desc = "resume picker" })
        '';
      }
      vim-tmux-navigator
      {
        plugin = yanky-nvim;
        type = "lua";
        config = ''
          require("yanky").setup({
            ring = { storage = "shada" },
            highlight = { on_put = true, on_yank = true },
          })
          pcall(require("telescope").load_extension, "yank_history")

          vim.keymap.set({ "n", "x" }, "y", "<Plug>(YankyYank)",      { desc = "yank" })
          vim.keymap.set({ "n", "x" }, "p", "<Plug>(YankyPutAfter)",  { desc = "put after" })
          vim.keymap.set({ "n", "x" }, "P", "<Plug>(YankyPutBefore)", { desc = "put before" })
          -- ]p / [p are yanky's documented defaults; avoids clobbering <c-p>/<c-n>
          -- which blink and built-in keyword completion both use.
          vim.keymap.set("n", "]p", "<Plug>(YankyCycleForward)",  { desc = "cycle yank forward" })
          vim.keymap.set("n", "[p", "<Plug>(YankyCycleBackward)", { desc = "cycle yank backward" })
          vim.keymap.set("n", "<leader>sy", "<cmd>Telescope yank_history<cr>", { desc = "yank history" })
        '';
      }
      {
        plugin = harpoon2;
        type = "lua";
        config = ''
          local harpoon = require("harpoon")
          harpoon:setup({})
          vim.keymap.set("n", "<leader>ha", function() harpoon:list():add() end, { desc = "harpoon add" })
          vim.keymap.set("n", "<leader>he", function() harpoon.ui:toggle_quick_menu(harpoon:list()) end, { desc = "harpoon menu" })
          vim.keymap.set("n", "<leader>1", function() harpoon:list():select(1) end, { desc = "harpoon 1" })
          vim.keymap.set("n", "<leader>2", function() harpoon:list():select(2) end, { desc = "harpoon 2" })
          vim.keymap.set("n", "<leader>3", function() harpoon:list():select(3) end, { desc = "harpoon 3" })
          vim.keymap.set("n", "<leader>4", function() harpoon:list():select(4) end, { desc = "harpoon 4" })
        '';
      }
      {
        plugin = grug-far-nvim;
        type = "lua";
        config = ''
          require("grug-far").setup({})
          vim.keymap.set("n", "<leader>sr", function() require("grug-far").open() end, { desc = "find/replace" })
          vim.keymap.set("n", "<leader>sw", function()
            require("grug-far").open({ prefills = { search = vim.fn.expand("<cword>") } })
          end, { desc = "find/replace word" })
          vim.keymap.set("v", "<leader>sr", function()
            require("grug-far").with_visual_selection()
          end, { desc = "find/replace selection" })
          vim.keymap.set("n", "<leader>sf", function()
            require("grug-far").open({ prefills = { paths = vim.fn.expand("%") } })
          end, { desc = "find/replace in file" })
        '';
      }
      # Git
      {
        plugin = diffview-nvim;
        type = "lua";
        config = ''
          require("diffview").setup({})
          vim.keymap.set("n", "<leader>gv", "<cmd>DiffviewOpen<cr>", { desc = "diffview" })
          vim.keymap.set("n", "<leader>gV", "<cmd>DiffviewClose<cr>", { desc = "diffview close" })
          vim.keymap.set("n", "<leader>gh", "<cmd>DiffviewFileHistory %<cr>", { desc = "file history" })
          vim.keymap.set("n", "<leader>gH", "<cmd>DiffviewFileHistory<cr>", { desc = "branch history" })
        '';
      }
      {
        plugin = git-conflict-nvim;
        type = "lua";
        config = ''
          require("git-conflict").setup({
            default_mappings = true,
            default_commands = true,
          })
          vim.keymap.set("n", "<leader>gx", "<cmd>GitConflictListQf<cr>", { desc = "list conflicts" })
        '';
      }

      # Editing
      vim-repeat
      vim-sleuth
      # Disable treesitter/lsp/illuminate/etc. on files >5 MiB so opening
      # minified JSON or large generated artifacts doesn't hang nvim.
      # matchparen is intentionally omitted from `features` -- upstream
      # leaves it disabled session-wide once tripped, which breaks `%` for
      # normal files in the same session.
      {
        plugin = bigfile-nvim;
        type = "lua";
        config = ''
          require("bigfile").setup({
            filesize = 5,
            features = {
              "indent_blankline",
              "illuminate",
              "lsp",
              "treesitter",
              "syntax",
              "vimopts",
              "filetype",
              {
                name = "mini_indentscope",
                opts = { defer = true },
                disable = function(buf)
                  vim.b[buf].miniindentscope_disable = true
                end,
              },
            },
          })
        '';
      }
      {
        plugin = vim-illuminate;
        type = "lua";
        config = ''
          require("illuminate").configure({
            providers = { "lsp", "treesitter", "regex" },
            delay = 100,
            filetypes_denylist = {
              "minifiles", "starter", "TelescopePrompt", "trouble", "Trouble",
              "lazy", "mason", "notify", "help",
            },
            under_cursor = false,
          })
        '';
      }
      {
        plugin = multicursor-nvim;
        type = "lua";
        config = ''
          local mc = require("multicursor-nvim")
          mc.setup()

          vim.keymap.set({ "n", "x" }, "<up>",           function() mc.lineAddCursor(-1) end,  { desc = "add cursor up" })
          vim.keymap.set({ "n", "x" }, "<down>",         function() mc.lineAddCursor(1) end,   { desc = "add cursor down" })
          vim.keymap.set({ "n", "x" }, "<leader><up>",   function() mc.lineSkipCursor(-1) end, { desc = "skip cursor up" })
          vim.keymap.set({ "n", "x" }, "<leader><down>", function() mc.lineSkipCursor(1) end,  { desc = "skip cursor down" })

          vim.keymap.set({ "n", "x" }, "<leader>mn", function() mc.matchAddCursor(1) end,    { desc = "match add next" })
          vim.keymap.set({ "n", "x" }, "<leader>mN", function() mc.matchAddCursor(-1) end,   { desc = "match add prev" })
          vim.keymap.set({ "n", "x" }, "<leader>ms", function() mc.matchSkipCursor(1) end,   { desc = "match skip next" })
          vim.keymap.set({ "n", "x" }, "<leader>mS", function() mc.matchSkipCursor(-1) end,  { desc = "match skip prev" })
          vim.keymap.set({ "n", "x" }, "<leader>mA", mc.matchAllAddCursors,                  { desc = "match all" })
          vim.keymap.set("n",          "<leader>ma", mc.alignCursors,                        { desc = "align cursors" })
          vim.keymap.set("x",          "<leader>mt", function() mc.transposeCursors(1) end,  { desc = "transpose >" })
          vim.keymap.set("x",          "<leader>mT", function() mc.transposeCursors(-1) end, { desc = "transpose <" })
          vim.keymap.set("n",          "<leader>mr", mc.restoreCursors,                      { desc = "restore cursors" })

          vim.keymap.set("n", "<c-leftmouse>",   mc.handleMouse)
          vim.keymap.set("n", "<c-leftdrag>",    mc.handleMouseDrag)
          vim.keymap.set("n", "<c-leftrelease>", mc.handleMouseRelease)

          vim.keymap.set({ "n", "x" }, "<c-q>", mc.toggleCursor, { desc = "toggle cursor" })

          mc.addKeymapLayer(function(layerSet)
            layerSet({ "n", "x" }, "<left>",  mc.prevCursor)
            layerSet({ "n", "x" }, "<right>", mc.nextCursor)
            layerSet({ "n", "x" }, "<leader>x", mc.deleteCursor)
            layerSet("n", "<esc>", function()
              if not mc.cursorsEnabled() then
                mc.enableCursors()
              else
                mc.clearCursors()
              end
            end)
          end)
        '';
      }

      # Learning -- which-key auto-discovers `desc` from vim.keymap.set;
      # we only need to declare the <leader>g group label here.
      {
        plugin = which-key-nvim;
        type = "lua";
        config = ''
          local wk = require("which-key")
          wk.setup({ preset = "modern", delay = 500 })
          wk.add({
            { "<leader>g", group = "git",             icon = { icon = "${icons.git} ",      color = "orange" } },
            { "<leader>t", group = "treesitter",      icon = { icon = "${icons.tree} ",     color = "green"  } },
            { "<leader>c", group = "code",            icon = { icon = "${icons.code} ",     color = "orange" } },
            { "<leader>r", group = "refactor/resume", icon = { icon = "${icons.refresh} ",  color = "cyan"   } },
            { "<leader>s", group = "search/symbols",  icon = { icon = "${icons.search} ",   color = "green"  } },
            { "<leader>x", group = "trouble",         icon = { icon = "${icons.fileTree} ", color = "red"    } },
            { "<leader>q", group = "session",         icon = { icon = "${icons.save} ",     color = "azure"  } },
            { "<leader>n", group = "noice",           icon = { icon = "${icons.fire} ",     color = "orange" } },
            { "<leader>h", group = "harpoon",         icon = { icon = "${icons.harpoon} ",  color = "cyan"   } },
            { "<leader>m", group = "multicursor",     icon = { icon = "${icons.cursor} ",   color = "yellow" } },
          })
        '';
      }

      # Markdown rendering
      {
        plugin = render-markdown-nvim;
        type = "lua";
        config = ''
          require("render-markdown").setup({})
        '';
      }

      # Inline color preview (#rrggbb, rgb(), hsl(), Tailwind classes)
      {
        plugin = nvim-highlight-colors;
        type = "lua";
        config = ''
          require("nvim-highlight-colors").setup({
            render = "virtual",
            enable_tailwind = true,
          })
        '';
      }

      # Treesitter (replaces vim-polyglot). main-branch nvim-treesitter no longer
      # exposes nvim-treesitter.configs; highlight/indent are wired up directly
      # against vim.treesitter, and textobjects has its own setup call.
      {
        plugin = nvim-treesitter-context;
        type = "lua";
        config = ''
          require("treesitter-context").setup({ max_lines = 3 })
        '';
      }
      {
        plugin = nvim-treesitter.withAllGrammars;
        type = "lua";
        config = ''
          vim.api.nvim_create_autocmd("FileType", {
            callback = function(args)
              local lang = vim.treesitter.language.get_lang(vim.bo[args.buf].filetype)
              if not lang or not pcall(vim.treesitter.start, args.buf, lang) then
                return
              end
              vim.bo[args.buf].indentexpr = "v:lua.require'nvim-treesitter'.indentexpr()"
            end,
          })
        '';
      }
      {
        plugin = nvim-treesitter-textobjects;
        type = "lua";
        config = ''
          require("nvim-treesitter-textobjects").setup({
            select = { lookahead = true },
            move = { set_jumps = true },
          })

          local select = require("nvim-treesitter-textobjects.select")
          local move = require("nvim-treesitter-textobjects.move")

          local function sel(query)
            return function() select.select_textobject(query, "textobjects") end
          end

          for _, mode in ipairs({ "x", "o" }) do
            vim.keymap.set(mode, "af", sel("@function.outer"), { desc = "function outer" })
            vim.keymap.set(mode, "if", sel("@function.inner"), { desc = "function inner" })
            vim.keymap.set(mode, "ac", sel("@class.outer"), { desc = "class outer" })
            vim.keymap.set(mode, "ic", sel("@class.inner"), { desc = "class inner" })
            vim.keymap.set(mode, "aa", sel("@parameter.outer"), { desc = "parameter outer" })
            vim.keymap.set(mode, "ia", sel("@parameter.inner"), { desc = "parameter inner" })
          end

          vim.keymap.set("n", "]f", function() move.goto_next_start("@function.outer", "textobjects") end, { desc = "next function" })
          vim.keymap.set("n", "]a", function() move.goto_next_start("@parameter.inner", "textobjects") end, { desc = "next parameter" })
          vim.keymap.set("n", "[f", function() move.goto_previous_start("@function.outer", "textobjects") end, { desc = "prev function" })
          vim.keymap.set("n", "[a", function() move.goto_previous_start("@parameter.inner", "textobjects") end, { desc = "prev parameter" })
        '';
      }

      {
        plugin = nvim-ts-autotag;
        type = "lua";
        config = ''require("nvim-ts-autotag").setup({})'';
      }

      # Folding driven by treesitter (with indent fallback). Fold options live
      # in init.lua so they're set at startup before any buffer opens.
      promise-async
      {
        plugin = nvim-ufo;
        type = "lua";
        config = ''
          require("ufo").setup({
            provider_selector = function(bufnr, filetype, buftype)
              return { "treesitter", "indent" }
            end,
          })
          vim.keymap.set("n", "zR", require("ufo").openAllFolds,  { desc = "open all folds" })
          vim.keymap.set("n", "zM", require("ufo").closeAllFolds, { desc = "close all folds" })
        '';
      }

      # Replaces the incremental_selection feature dropped from nvim-treesitter
      # main. <C-space> grows the selection up the tree; <BS> shrinks it back.
      {
        plugin = wildfire-nvim;
        type = "lua";
        config = ''
          require("wildfire").setup({
            keymaps = {
              init_selection = "<C-space>",
              node_incremental = "<C-space>",
              node_decremental = "<BS>",
            },
          })
        '';
      }

      # Snippets (loaded before blink-cmp so the source has data;
      # mini.snippets is set up in the mini-nvim block at the top).
      friendly-snippets

      # Completion (must come before lspconfig so capabilities are available)
      {
        plugin = blink-cmp;
        type = "lua";
        config = ''
          require("blink.cmp").setup({
            keymap = { preset = "default" },
            completion = {
              documentation = { auto_show = true, auto_show_delay_ms = 200 },
              ghost_text = { enabled = true },
            },
            signature = { enabled = true },
            snippets = { preset = "mini_snippets" },
            sources = {
              default = { "lazydev", "lsp", "path", "snippets", "buffer" },
              providers = {
                lazydev = {
                  name = "LazyDev",
                  module = "lazydev.integrations.blink",
                  score_offset = 100,
                },
              },
            },
          })
        '';
      }

      # JSON/YAML schemas (loaded before lspconfig so it can require("schemastore"))
      SchemaStore-nvim

      # LSP
      {
        plugin = fidget-nvim;
        type = "lua";
        config = ''require("fidget").setup({})'';
      }
      {
        plugin = lazydev-nvim;
        type = "lua";
        config = ''
          require("lazydev").setup({
            library = {
              { path = "''${3rd}/luv/library", words = { "vim%.uv" } },
            },
          })
        '';
      }
      {
        plugin = nvim-lspconfig;
        type = "lua";
        config = ''
          vim.diagnostic.config({
            virtual_text = { prefix = "●" },
            severity_sort = true,
            float = { border = "rounded", source = true },
          })

          vim.api.nvim_create_autocmd("LspAttach", {
            callback = function(ev)
              local tb = require("telescope.builtin")
              local map = function(mode, lhs, rhs, desc)
                vim.keymap.set(mode, lhs, rhs, { buffer = ev.buf, desc = desc })
              end
              map("n", "gd", tb.lsp_definitions, "definition")
              map("n", "gD", vim.lsp.buf.declaration, "declaration")
              map("n", "gr", tb.lsp_references, "references")
              map("n", "gi", tb.lsp_implementations, "implementation")
              map("n", "gy", tb.lsp_type_definitions, "type definition")
              map("n", "K", vim.lsp.buf.hover, "hover")
              map("n", "<leader>rn", vim.lsp.buf.rename, "rename")
              map({ "n", "v" }, "<leader>ca", vim.lsp.buf.code_action, "code action")
              map("n", "<leader>e", vim.diagnostic.open_float, "diagnostic float")
              map("n", "<leader>ss", tb.lsp_document_symbols, "document symbols")
              map("n", "<leader>sS", tb.lsp_dynamic_workspace_symbols, "workspace symbols")
              map("n", "[d", function() vim.diagnostic.jump({ count = -1, float = true }) end, "prev diagnostic")
              map("n", "]d", function() vim.diagnostic.jump({ count = 1, float = true }) end, "next diagnostic")
            end,
          })

          vim.lsp.config("*", {
            capabilities = require("blink.cmp").get_lsp_capabilities(),
          })

          local servers = {
            gopls = {},
            nixd = {},
            bashls = {},
            pyright = {},
            taplo = {},
            marksman = {},
            dockerls = {},
            terraformls = {},
            lua_ls = {
              settings = {
                Lua = {
                  workspace = { checkThirdParty = false },
                  telemetry = { enable = false },
                },
              },
            },
            yamlls = {
              settings = {
                yaml = {
                  schemaStore = { enable = false, url = "" },
                  schemas = require("schemastore").yaml.schemas(),
                  validate = true,
                  format = { enable = true },
                },
              },
            },
            jsonls = {
              settings = {
                json = {
                  schemas = require("schemastore").json.schemas(),
                  validate = { enable = true },
                },
              },
            },
          }
          for name, cfg in pairs(servers) do
            vim.lsp.config(name, cfg)
          end
          vim.lsp.enable(vim.tbl_keys(servers))
        '';
      }

      # Linting (LSP-adjacent diagnostics from external linters)
      {
        plugin = nvim-lint;
        type = "lua";
        config = ''
          require("lint").linters_by_ft = {
            sh = { "shellcheck" },
            bash = { "shellcheck" },
            nix = { "statix", "deadnix" },
            yaml = { "yamllint" },
          }
          vim.api.nvim_create_autocmd({ "BufWritePost", "BufReadPost", "InsertLeave" }, {
            callback = function() require("lint").try_lint() end,
          })
        '';
      }

      # Diagnostics / quickfix list
      {
        plugin = trouble-nvim;
        type = "lua";
        config = ''
          require("trouble").setup({})
          vim.keymap.set("n", "<leader>xx", "<cmd>Trouble diagnostics toggle<cr>", { desc = "diagnostics" })
          vim.keymap.set("n", "<leader>xX", "<cmd>Trouble diagnostics toggle filter.buf=0<cr>", { desc = "buffer diagnostics" })
          vim.keymap.set("n", "<leader>xs", "<cmd>Trouble symbols toggle focus=false<cr>", { desc = "symbols" })
          vim.keymap.set("n", "<leader>xl", "<cmd>Trouble lsp toggle focus=false win.position=right<cr>", { desc = "LSP refs/defs" })
          vim.keymap.set("n", "<leader>xL", "<cmd>Trouble loclist toggle<cr>", { desc = "location list" })
          vim.keymap.set("n", "<leader>xQ", "<cmd>Trouble qflist toggle<cr>", { desc = "quickfix list" })
        '';
      }

      # TODO/FIXME/HACK highlights and search (loaded after trouble for :TodoTrouble)
      {
        plugin = todo-comments-nvim;
        type = "lua";
        config = ''
          local todo = require("todo-comments")
          todo.setup({})
          vim.keymap.set("n", "]t", function() todo.jump_next() end, { desc = "next todo" })
          vim.keymap.set("n", "[t", function() todo.jump_prev() end, { desc = "prev todo" })
          vim.keymap.set("n", "<leader>st", "<cmd>TodoTelescope<cr>", { desc = "todos" })
          vim.keymap.set("n", "<leader>xt", "<cmd>TodoTrouble<cr>", { desc = "todos (trouble)" })
        '';
      }

      # UI: cmdline, messages, popupmenu, LSP routing (depends on nui;
      # vim.notify is provided by mini.notify, set up in the mini-nvim block).
      nui-nvim
      {
        plugin = noice-nvim;
        type = "lua";
        config = ''
          require("noice").setup({
            lsp = {
              override = {
                ["vim.lsp.util.convert_input_to_markdown_lines"] = true,
                ["vim.lsp.util.stylize_markdown"] = true,
                ["cmp.entry.get_documentation"] = true,
              },
            },
            presets = {
              bottom_search = true,
              command_palette = true,
              long_message_to_split = true,
              lsp_doc_border = true,
            },
          })
          vim.keymap.set("n", "<leader>nl", "<cmd>NoiceLast<cr>", { desc = "last message" })
          vim.keymap.set("n", "<leader>nh", "<cmd>NoiceHistory<cr>", { desc = "message history" })
          vim.keymap.set("n", "<leader>nd", "<cmd>NoiceDismiss<cr>", { desc = "dismiss messages" })
        '';
      }

      # Formatting
      {
        plugin = conform-nvim;
        type = "lua";
        config = ''
          require("conform").setup({
            formatters_by_ft = {
              lua = { "stylua" },
              nix = { "nixfmt" },
              go = { "goimports", "gofumpt" },
              sh = { "shfmt" },
              bash = { "shfmt" },
              json = { "prettierd" },
              jsonc = { "prettierd" },
              yaml = { "prettierd" },
              markdown = { "prettierd" },
              html = { "prettierd" },
              css = { "prettierd" },
              scss = { "prettierd" },
              javascript = { "prettierd" },
              javascriptreact = { "prettierd" },
              typescript = { "prettierd" },
              typescriptreact = { "prettierd" },
              vue = { "prettierd" },
              svelte = { "prettierd" },
              terraform = { "tofu_fmt" },
              python = { "ruff_format" },
            },
            format_on_save = {
              timeout_ms = 1000,
              lsp_format = "fallback",
            },
          })
          vim.keymap.set({ "n", "v" }, "<leader>F", function()
            require("conform").format({ async = true, lsp_format = "fallback" })
          end, { desc = "format" })
        '';
      }
    ];

    initLua = builtins.readFile ../configs/nvim/init.lua;
  };

  # Cheat sheet shown in the tmux hints pane when nvim is the active command
  # (tmux-hints.sh reads ~/.config/hints/$cmd.txt).
  xdg.configFile."hints/nvim.txt".source = ../configs/hints/nvim.txt;
}
