#!/usr/bin/env python3
"""Draws assets/dmg-background.png, the picture behind the icons in the disk image.

The picture is 1200x800 pixels tagged 144 dpi, which the Finder shows as a
600x400 point window on every screen. The Finder draws the icon labels in
black over a picture in both appearances, so the ground is light.

    python3 tools/make_dmg_background.py
"""
import math
import sys
from pathlib import Path

from PIL import Image, ImageChops, ImageDraw, ImageFilter

WIDTH, HEIGHT = 600, 400          # points
SCALE = 2                         # pixels per point of the file
SUPER = 4                         # supersampling for the arrow
APP_X, APPLICATIONS_X, ICON_Y = 160, 440, 185

TOP_LEFT = (238, 248, 242)
BOTTOM_RIGHT = (198, 226, 211)
ARROW = (38, 150, 102)


def ground(width, height):
    """A diagonal gradient with a soft light in the middle, as one RGB image."""
    image = Image.new("RGB", (width, height))
    pixels = image.load()
    for y in range(height):
        for x in range(width):
            t = (x / width * 0.6 + y / height * 0.4)
            r = [TOP_LEFT[i] + (BOTTOM_RIGHT[i] - TOP_LEFT[i]) * t for i in range(3)]
            dx, dy = (x / width - 0.5) / 0.6, (y / height - 0.46) / 0.7
            glow = max(0.0, 1.0 - math.hypot(dx, dy)) ** 2 * 10
            pixels[x, y] = tuple(min(255, int(c + glow)) for c in r)
    return image


def arrow(width, height):
    """A white arrow between the two icons on a transparent layer."""
    k = SUPER * SCALE
    layer = Image.new("L", (width * SUPER, height * SUPER), 0)
    draw = ImageDraw.Draw(layer)
    y = ICON_Y * k
    left, right = (APP_X + 92) * k, (APPLICATIONS_X - 92) * k
    thickness, head = 9 * k, 22 * k
    draw.line([(left, y), (right - thickness * 0.4, y)], fill=255, width=thickness)
    draw.ellipse([left - thickness / 2, y - thickness / 2, left + thickness / 2, y + thickness / 2], fill=255)
    draw.line([(right - head, y - head), (right, y), (right - head, y + head)], fill=255, width=thickness, joint="curve")
    for dy in (-head, head):
        cx, cy = right - head, y + dy
        draw.ellipse([cx - thickness / 2, cy - thickness / 2, cx + thickness / 2, cy + thickness / 2], fill=255)
    draw.ellipse([right - thickness / 2, y - thickness / 2, right + thickness / 2, y + thickness / 2], fill=255)
    return layer.resize((width, height), Image.LANCZOS)


def main():
    width, height = WIDTH * SCALE, HEIGHT * SCALE
    image = ground(width, height)
    mask = arrow(width, height)
    shadow = mask.filter(ImageFilter.GaussianBlur(3 * SCALE)).point(lambda v: int(v * 0.18))
    shadow = ImageChops.offset(shadow, 0, 2 * SCALE)
    image.paste((40, 90, 66), mask=shadow)
    image.paste(ARROW, mask=mask.point(lambda v: int(v * 0.95)))
    out = Path(__file__).resolve().parent.parent / "assets" / "dmg-background.png"
    image.save(out, dpi=(72 * SCALE, 72 * SCALE), optimize=True)
    print(f"wrote {out} ({width}x{height} px, {WIDTH}x{HEIGHT} pt)")


if __name__ == "__main__":
    sys.exit(main())
