{ lib }:
let
  inherit (builtins) floor fromJSON;
  abs = x: if x < 0.0 then -x else x;
  fmod = x: y: x - y * floor (x / y);
in
{
  # Convert a stylix base16 color to HSL { h s l } (integer values).
  # Usage: rgbToHsl colors "base0E"
  rgbToHsl =
    colors: name:
    let
      r = fromJSON colors."${name}-rgb-r" / 255.0;
      g = fromJSON colors."${name}-rgb-g" / 255.0;
      b = fromJSON colors."${name}-rgb-b" / 255.0;
      max = lib.max r (lib.max g b);
      min = lib.min r (lib.min g b);
      delta = max - min;
      l = (max + min) / 2.0;
      s = if delta == 0.0 then 0.0 else delta / (1.0 - abs (2.0 * l - 1.0));
      h =
        if delta == 0.0 then
          0.0
        else if max == r then
          60.0 * (fmod ((g - b) / delta) 6.0)
        else if max == g then
          60.0 * ((b - r) / delta + 2.0)
        else
          60.0 * ((r - g) / delta + 4.0);
    in
    {
      h = floor (if h < 0.0 then h + 360.0 else h);
      s = floor (s * 100.0);
      l = floor (l * 100.0);
    };

  # Convert a stylix base16 color to an RGB attrset { r g b } (integer values).
  # Usage: rgbToAttrs colors "base08"
  rgbToAttrs = colors: name: {
    r = fromJSON colors."${name}-rgb-r";
    g = fromJSON colors."${name}-rgb-g";
    b = fromJSON colors."${name}-rgb-b";
  };
}
