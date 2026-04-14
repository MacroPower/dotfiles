{
  lib,
  buildDotnetGlobalTool,
}:

buildDotnetGlobalTool {
  pname = "photo-cli";
  version = "0.3.3";

  nugetHash = "sha256-6we5MWr/I2qoRWPjI+ag1Dar7CdBVPb8HYf79Vphl2s=";

  meta = {
    description = "Photo organizer: extract EXIF dates/locations, copy into organized folders";
    homepage = "https://github.com/photo-cli/photo-cli";
    license = lib.licenses.mit;
    mainProgram = "photo-cli";
  };
}
