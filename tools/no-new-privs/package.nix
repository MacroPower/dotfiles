{
  lib,
  buildGoModule,
}:

buildGoModule {
  pname = "no-new-privs";
  version = "0.1.0";

  src = ./.;
  vendorHash = "sha256-LXR8/S1x5FOxgcp8uXppc2foxwHZq6KANA3WCtX0MoE=";

  meta = {
    description = "Wrapper that sets PR_SET_NO_NEW_PRIVS before exec-ing a command";
    license = lib.licenses.asl20;
    mainProgram = "no-new-privs";
    platforms = lib.platforms.linux;
  };
}
