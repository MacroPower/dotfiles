{ lib }:
let
  inherit (builtins) floor fromJSON;
  abs = x: if x < 0.0 then -x else x;
  fmod = x: y: x - y * floor (x / y);

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

  hslToRgb =
    {
      h,
      s,
      l,
    }:
    let
      sf = s / 100.0;
      lf = l / 100.0;
      c = (1.0 - abs (2.0 * lf - 1.0)) * sf;
      hp = h / 60.0;
      x = c * (1.0 - abs (fmod hp 2.0 - 1.0));
      m = lf - c / 2.0;
      triple =
        if h < 60 then
          {
            r = c;
            g = x;
            b = 0.0;
          }
        else if h < 120 then
          {
            r = x;
            g = c;
            b = 0.0;
          }
        else if h < 180 then
          {
            r = 0.0;
            g = c;
            b = x;
          }
        else if h < 240 then
          {
            r = 0.0;
            g = x;
            b = c;
          }
        else if h < 300 then
          {
            r = x;
            g = 0.0;
            b = c;
          }
        else
          {
            r = c;
            g = 0.0;
            b = x;
          };
      to8 = v: floor ((v + m) * 255.0 + 0.5);
    in
    {
      r = to8 triple.r;
      g = to8 triple.g;
      b = to8 triple.b;
    };

  toHexByte =
    n:
    let
      hex = "0123456789abcdef";
      clamped =
        if n < 0 then
          0
        else if n > 255 then
          255
        else
          n;
      hi = clamped / 16;
      lo = clamped - hi * 16;
    in
    builtins.substring hi 1 hex + builtins.substring lo 1 hex;
in
{
  # Convert a stylix base16 color to HSL { h s l } (integer values).
  # Usage: rgbToHsl colors "base0E"
  inherit rgbToHsl;

  # Convert a stylix base16 color to an RGB attrset { r g b } (integer values).
  # Usage: rgbToAttrs colors "base08"
  rgbToAttrs = colors: name: {
    r = fromJSON colors."${name}-rgb-r";
    g = fromJSON colors."${name}-rgb-g";
    b = fromJSON colors."${name}-rgb-b";
  };

  # Return a "#rrggbb" string for a base16 color lightened by deltaL
  # percentage points in HSL space. Negative deltaL darkens.
  # Usage: lighten colors "base09" 12
  lighten =
    colors: name: deltaL:
    let
      hsl = rgbToHsl colors name;
      raw = hsl.l + deltaL;
      l' =
        if raw > 100 then
          100
        else if raw < 0 then
          0
        else
          raw;
      rgb = hslToRgb {
        inherit (hsl) h s;
        l = l';
      };
    in
    "#${toHexByte rgb.r}${toHexByte rgb.g}${toHexByte rgb.b}";
}
