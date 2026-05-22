{ writeShellApplication, cmark-gfm }:

writeShellApplication {
  name = "mdcopy";
  runtimeInputs = [ cmark-gfm ];
  text = ''
    cmark-gfm -e table -e strikethrough -e autolink -e tasklist "$@" \
      | hexdump -ve '1/1 "%.2x"' \
      | xargs printf 'set the clipboard to {string:" ", «class HTML»:«data HTML%s»}' \
      | osascript -
  '';
}
