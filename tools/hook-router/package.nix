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
    license = lib.licenses.asl20;
    mainProgram = "hook-router";
  };
}
