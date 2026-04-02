{
  lib,
  buildGoModule,
}:

buildGoModule {
  pname = "ensure-docker-proxy";
  version = "0.1.0";

  src = ./.;
  vendorHash = "sha256-5kBzgzcXUXruMeTKbu8o1q6qJ9yVp3dCBYxkbaCrRt4=";

  meta = {
    description = "Ensures a Docker socket proxy container is running for Claude Code sessions";
    license = lib.licenses.asl20;
    mainProgram = "ensure-docker-proxy";
  };
}
