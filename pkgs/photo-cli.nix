{
  lib,
  buildDotnetGlobalTool,
}:

buildDotnetGlobalTool {
  pname = "photo-cli";
  version = "0.4.0";

  nugetHash = "sha256-WnblYclz249HlMlIZihEK0GWHWHqynmuwcWr20zzluY=";

  meta = {
    description = "Photo organizer: extract EXIF dates/locations, copy into organized folders";
    homepage = "https://github.com/photo-cli/photo-cli";
    license = lib.licenses.mit;
    mainProgram = "photo-cli";
  };
}
