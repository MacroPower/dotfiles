{
  lib,
  buildGoModule,
  git,
}:

buildGoModule {
  pname = "hook-router";
  version = "0.2.0";

  src = ./.;
  vendorHash = "sha256-nTXOVjcInXYVCDbQzZCIAuDIYEXFYCaHOv2d8ESIrJg=";
  proxyVendor = true;

  nativeCheckInputs = [ git ];

  meta = {
    description = "Claude Code hook router with plan-guard lifecycle management";
    homepage = "https://github.com/MacroPower/dotfiles/tree/main/tools/hook-router";
    license = lib.licenses.asl20;
    mainProgram = "hook-router";
  };
}
