{
  lib,
  buildGoModule,
}:

buildGoModule {
  pname = "hook-router";
  version = "0.1.0";

  src = ./.;
  vendorHash = "sha256-MClAhfWtyHbZcAR9gwcyLUoQD3ZlXSx1QcT279m3J2w=";

  meta = {
    description = "Claude Code PreToolUse hook router with shell AST rewriting";
    license = lib.licenses.asl20;
    mainProgram = "hook-router";
  };
}
