{
  lib,
  buildGoModule,
}:

buildGoModule {
  pname = "cookie";
  version = "0.1.0";

  src = ./.;
  vendorHash = "sha256-yqtj+zCo7u2UwaQ12bHCHPNucgQNkvkN7nfkLynB67Y=";

  meta = {
    description = "Tiny self-contained fortune cookie generator";
    license = lib.licenses.asl20;
    mainProgram = "cookie";
  };
}
