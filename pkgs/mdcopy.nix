{ writeShellApplication, cmark-gfm }:

writeShellApplication {
  name = "mdcopy";
  runtimeInputs = [ cmark-gfm ];
  text = ''
    cmark-gfm -e table -e strikethrough -e autolink -e tasklist -e tagfilter "$@" \
      | textutil -stdin -stdout -format html -inputencoding UTF-8 -convert rtf \
      | pbcopy
  '';
}
