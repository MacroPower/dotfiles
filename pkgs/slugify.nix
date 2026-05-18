{ writeShellScriptBin }:

writeShellScriptBin "slugify" ''
  echo "$*" | tr '[:upper:]' '[:lower:]' | tr -cs '[:alnum:]' '-' | sed 's/^-//;s/-$//' | cut -c1-60
''
