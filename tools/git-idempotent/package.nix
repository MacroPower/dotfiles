{
  lib,
  buildGoModule,
}:

buildGoModule {
  pname = "git-idempotent";
  version = "0.1.0";

  src = ./.;
  vendorHash = "sha256-yqtj+zCo7u2UwaQ12bHCHPNucgQNkvkN7nfkLynB67Y=";

  meta = {
    description = "Idempotent git clone: clones if missing, pulls if present";
    license = lib.licenses.asl20;
    mainProgram = "git-idempotent";
  };
}
