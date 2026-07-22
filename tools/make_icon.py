# -*- coding: utf-8 -*-
"""Draw the application icon.

It is the panel's own logo — the same rounded orange square with two server
shelves that the sidebar and the favicon use — so the .exe in the server folder
and the page in the browser read as one product.

Two variants are drawn, because an outline that looks right at 256px vanishes
at 16px: large sizes get the outlined shelves with indicator dots, small sizes
get solid bars. Everything is drawn at 4x and downsampled, which is the only
way to get clean edges out of PIL.

The .ico is assembled by hand. PIL's ICO writer ignores `append_images` and
silently keeps ONE image, which is how a single 16x16 entry ended up shipping
in v0.19.0 — Windows then upscaled that everywhere. Writing the directory
ourselves is a dozen lines and makes the result verifiable.
"""
from PIL import Image, ImageDraw
import io, os, struct

HERE = os.path.dirname(os.path.abspath(__file__))
OUT = os.path.join(HERE, 'icon.ico')

# The panel's tokens: --accent #ff7a2b with the logo's gradient, dark ink.
ORANGE_TOP = (255, 143, 77)
ORANGE_BOT = (239, 106, 21)
INK = (20, 8, 10)


def rounded_rect_mask(size, radius, scale):
    m = Image.new('L', (size * scale, size * scale), 0)
    d = ImageDraw.Draw(m)
    d.rounded_rectangle([0, 0, size * scale - 1, size * scale - 1],
                        radius=radius * scale, fill=255)
    return m


def gradient(size, scale):
    """Vertical orange gradient, matching .sidebar-brand .logo."""
    g = Image.new('RGB', (1, size * scale))
    for y in range(size * scale):
        t = y / max(1, size * scale - 1)
        g.putpixel((0, y), tuple(
            round(ORANGE_TOP[i] + (ORANGE_BOT[i] - ORANGE_TOP[i]) * t) for i in range(3)))
    return g.resize((size * scale, size * scale))


def draw_icon(size):
    scale = 4
    S = size * scale
    img = gradient(size, scale).convert('RGBA')
    img.putalpha(rounded_rect_mask(size, max(2, round(size * 0.22)), scale))

    d = ImageDraw.Draw(img)
    # Geometry from the favicon: two shelves inset from the edges.
    inset_x = S * 5.5 / 24
    w = S * 13 / 24
    h = S * 5.5 / 24
    y1 = S * 5.6 / 24
    y2 = S * 12.9 / 24
    r = S * 1.5 / 24

    if size >= 48:
        # Outlined shelves with an indicator light, as on screen.
        # Thinner than the SVG's 1.8: at icon sizes a heavy stroke closes the
        # shelf's interior and the shape stops reading as a rack.
        lw = max(scale, round(S * 1.35 / 24))
        for y in (y1, y2):
            d.rounded_rectangle([inset_x, y, inset_x + w, y + h], radius=r,
                                outline=INK, width=lw)
        dot = S * 0.85 / 24
        for cy in (y1 + h / 2, y2 + h / 2):
            cx = S * 8.5 / 24
            d.ellipse([cx - dot, cy - dot, cx + dot, cy + dot], fill=INK)
    else:
        # At 16–32px an outline turns to mush; solid bars stay legible.
        for y in (y1, y2):
            d.rounded_rectangle([inset_x, y, inset_x + w, y + h], radius=r, fill=INK)

    return img.resize((size, size), Image.LANCZOS)


def write_ico(path, images):
    """Assemble a multi-resolution .ico with PNG-compressed entries.

    Every payload is a PNG, which Windows Vista and later accept at any size.
    Header: 6 bytes, then one 16-byte directory entry per image, then the
    payloads. A dimension of 256 is stored as 0 — that is the format's way of
    fitting 256 into one byte.
    """
    payloads = []
    for im in images:
        buf = io.BytesIO()
        im.save(buf, format='PNG', optimize=True)
        payloads.append(buf.getvalue())

    offset = 6 + 16 * len(images)
    out = bytearray(struct.pack('<HHH', 0, 1, len(images)))
    for im, data in zip(images, payloads):
        w = 0 if im.width >= 256 else im.width
        h = 0 if im.height >= 256 else im.height
        out += struct.pack('<BBBBHHII', w, h, 0, 0, 1, 32, len(data), offset)
        offset += len(data)
    for data in payloads:
        out += data
    open(path, 'wb').write(bytes(out))


SIZES = [16, 20, 24, 32, 48, 64, 128, 256]
imgs = [draw_icon(s) for s in SIZES]
write_ico(OUT, imgs)

# Verify what was actually written, rather than trusting the writer.
d = open(OUT, 'rb').read()
_, _, cnt = struct.unpack('<HHH', d[:6])
got = []
for i in range(cnt):
    off = 6 + i * 16
    got.append(d[off] or 256)
print('wrote %s: %d entries %s, %d bytes' % (OUT, cnt, got, len(d)))
assert got == SIZES, 'directory does not match the requested sizes: %s' % got

prev = Image.new('RGBA', (16 + sum(s + 16 for s in SIZES), 300), (12, 15, 22, 255))
x = 16
for s, im in zip(SIZES, imgs):
    prev.paste(im, (x, 150 - s // 2), im)
    x += s + 16
prev.save(os.path.join(HERE, 'icon_preview.png'))
print('preview written')
