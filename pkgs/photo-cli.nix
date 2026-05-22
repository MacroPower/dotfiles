{
  lib,
  buildDotnetGlobalTool,
  dotnetCorePackages,
}:

buildDotnetGlobalTool {
  pname = "photo-cli";
  version = "0.4.0";

  nugetHash = "sha256-WnblYclz249HlMlIZihEK0GWHWHqynmuwcWr20zzluY=";

  # 0.4.0 targets net10.0; the default dotnet-sdk_8 can't read the tool layout
  # and aborts with "DotnetToolSettings.xml not found".
  dotnet-sdk = dotnetCorePackages.sdk_10_0;

  meta = {
    description = "Photo organizer: extract EXIF dates/locations, copy into organized folders";
    homepage = "https://github.com/photo-cli/photo-cli";
    license = lib.licenses.mit;
    mainProgram = "photo-cli";
  };
}
