#!/usr/bin/env python3
"""Run from the repository root with Pillow and windres available."""

from __future__ import annotations

import io
import math
import os
from pathlib import Path
import shutil
import struct
import subprocess
import sys

from PIL import Image, ImageDraw

ROOT = Path(__file__).resolve().parents[2]
ASSETS = ROOT / "assets"
STEM = "control-center-v2"
SIZES = (16, 20, 24, 32, 40, 48, 64, 128, 256)
APPLICATIONS = ("ccapp", "ControlCenter", "ccfnkey")

# All geometry uses the same 1024-unit coordinate system as the SVG.
VIEWBOX = 1024
BOARD = (96, 96, 928, 928)
BOARD_RADIUS = 192
CENTER = (512, 512)
RING_RADIUS = 264
RING_WIDTH = 96
C_ARC = (48, 312)
PROGRESS_ARC = (218, 248)
NEEDLE = ((478, 478), (648, 376), (546, 546))
HUB_RADIUS = 48
GRAPHITE = "#121E2B"
CYAN = "#06C9E7"
PALE_CYAN = "#C6F7FB"
GREEN = "#42E69C"


def circle_point(angle: float, radius: float = RING_RADIUS) -> tuple[float, float]:
    radians = math.radians(angle)
    return (
        CENTER[0] + radius * math.cos(radians),
        CENTER[1] + radius * math.sin(radians),
    )


def svg_arc(start: float, end: float) -> str:
    x1, y1 = circle_point(start)
    x2, y2 = circle_point(end)
    large = int(end - start > 180)
    return (
        f"M {x1:.6f} {y1:.6f} "
        f"A {RING_RADIUS} {RING_RADIUS} 0 {large} 1 {x2:.6f} {y2:.6f}"
    )


def make_svg() -> str:
    left, top, right, bottom = BOARD
    needle = " ".join(f"{x},{y}" for x, y in NEEDLE)
    return f'''<svg xmlns="http://www.w3.org/2000/svg" width="1024" height="1024" viewBox="0 0 {VIEWBOX} {VIEWBOX}" role="img" aria-labelledby="title desc">
  <title id="title">Control Center</title>
  <desc id="desc">A cyan C-shaped gauge, a pale cyan northeast pointer and a short green progress arc on a rounded dark graphite tile with a transparent exterior.</desc>
  <rect x="{left}" y="{top}" width="{right - left}" height="{bottom - top}" rx="{BOARD_RADIUS}" fill="{GRAPHITE}"/>
  <path d="{svg_arc(*C_ARC)}" fill="none" stroke="{CYAN}" stroke-width="{RING_WIDTH}" stroke-linecap="round"/>
  <path d="{svg_arc(*PROGRESS_ARC)}" fill="none" stroke="{GREEN}" stroke-width="{RING_WIDTH}" stroke-linecap="round"/>
  <polygon points="{needle}" fill="{PALE_CYAN}"/>
  <circle cx="{CENTER[0]}" cy="{CENTER[1]}" r="{HUB_RADIUS}" fill="{PALE_CYAN}"/>
</svg>
'''


def render(size: int, supersample: int = 8) -> Image.Image:
    canvas_size = size * supersample
    scale = canvas_size / VIEWBOX
    image = Image.new("RGBA", (canvas_size, canvas_size), (0, 0, 0, 0))
    draw = ImageDraw.Draw(image)

    def scaled(point: tuple[float, float]) -> tuple[float, float]:
        return point[0] * scale, point[1] * scale

    def disk(center: tuple[float, float], radius: float, color: str) -> None:
        x, y = scaled(center)
        r = radius * scale
        draw.ellipse((x - r, y - r, x + r, y + r), fill=color)

    def arc(angles: tuple[int, int], color: str) -> None:
        start, end = angles
        # Half-degree chords differ from the SVG stroke by < 0.003 SVG units.
        # Fill one annular polygon: very short thick line segments can leave
        # rasterizer seams, even when Pillow's curved line joints are enabled.
        steps = (end - start) * 2
        samples = [start + (end - start) * i / steps for i in range(steps + 1)]
        outer = [scaled(circle_point(angle, RING_RADIUS + RING_WIDTH / 2))
                 for angle in samples]
        inner = [scaled(circle_point(angle, RING_RADIUS - RING_WIDTH / 2))
                 for angle in reversed(samples)]
        draw.polygon(outer + inner, fill=color)
        disk(circle_point(start), RING_WIDTH / 2, color)
        disk(circle_point(end), RING_WIDTH / 2, color)

    draw.rounded_rectangle(tuple(value * scale for value in BOARD),
                           radius=BOARD_RADIUS * scale, fill=GRAPHITE)
    arc(C_ARC, CYAN)
    arc(PROGRESS_ARC, GREEN)
    draw.polygon([scaled(point) for point in NEEDLE], fill=PALE_CYAN)
    disk(CENTER, HUB_RADIUS, PALE_CYAN)
    return image.resize((size, size), Image.Resampling.LANCZOS)


def make_ico(frames: dict[int, Image.Image]) -> bytes:
    # PNG-backed ICO entries retain full 32-bit RGBA at every size, including 256.
    payloads = []
    for size in SIZES:
        buffer = io.BytesIO()
        frames[size].save(buffer, format="PNG")
        payloads.append(buffer.getvalue())
    directory = bytearray(struct.pack("<HHH", 0, 1, len(SIZES)))
    offset = 6 + 16 * len(SIZES)
    for size, payload in zip(SIZES, payloads):
        dimension = size if size < 256 else 0
        directory.extend(struct.pack("<BBBBHHII", dimension, dimension, 0, 0,
                                     1, 32, len(payload), offset))
        offset += len(payload)
    return bytes(directory) + b"".join(payloads)


def validate_ico(path: Path) -> None:
    data = path.read_bytes()
    if struct.unpack_from("<HHH", data) != (0, 1, len(SIZES)):
        raise RuntimeError(f"Invalid ICO directory: {path}")
    for index, size in enumerate(SIZES):
        width, height, colors, reserved, planes, bits, length, offset = (
            struct.unpack_from("<BBBBHHII", data, 6 + index * 16)
        )
        if ((width or 256, height or 256) != (size, size)
                or (colors, reserved, planes, bits) != (0, 0, 1, 32)):
            raise RuntimeError(f"Invalid {size}px / 32-bit ICO entry: {path}")
        with Image.open(io.BytesIO(data[offset:offset + length])) as frame:
            frame.load()
            if frame.size != (size, size) or frame.mode != "RGBA":
                raise RuntimeError(f"Invalid RGBA ICO payload at {size}px: {path}")
            if frame.getpixel((0, 0))[3] != 0:
                raise RuntimeError(f"ICO exterior is not transparent at {size}px")
            if frame.getchannel("A").getextrema() != (0, 255):
                raise RuntimeError(f"ICO alpha range is invalid at {size}px")
    with Image.open(path) as icon:
        if icon.ico.sizes() != {(size, size) for size in SIZES}:
            raise RuntimeError(f"Pillow cannot read all ICO sizes: {path}")
        for size in SIZES:
            icon.ico.getimage((size, size)).load()


def validate_resource(path: Path) -> None:
    data = path.read_bytes()
    if len(data) < 20 or struct.unpack_from("<H", data)[0] != 0x8664:
        raise RuntimeError(f"Resource is not an amd64 COFF object: {path}")
    section_count = struct.unpack_from("<H", data, 2)[0]
    optional_size = struct.unpack_from("<H", data, 16)[0]
    section_start = 20 + optional_size
    for index in range(section_count):
        offset = section_start + index * 40
        name = data[offset:offset + 8].rstrip(b"\0")
        if name.startswith(b".rsrc"):
            raw_size, raw_offset = struct.unpack_from("<II", data, offset + 16)
            if raw_size and raw_offset + raw_size <= len(data):
                return
    raise RuntimeError(f"Resource object has no populated .rsrc section: {path}")


def main() -> int:
    windres = shutil.which("windres")
    if windres is None:
        raise RuntimeError("windres not found on PATH; Windows icon resources cannot be generated")
    for directory in (ASSETS, *(ROOT / "cmd" / app for app in APPLICATIONS)):
        if not directory.is_dir():
            raise RuntimeError(f"Required output directory does not exist: {directory}")

    (ASSETS / f"{STEM}.svg").write_text(make_svg(), encoding="utf-8", newline="\n")
    render(1024, supersample=4).save(ASSETS / f"{STEM}.png", format="PNG")
    frames = {size: render(size) for size in SIZES}
    ico = ASSETS / f"{STEM}.ico"
    ico.write_bytes(make_ico(frames))
    validate_ico(ico)
    shutil.copyfile(ico, ROOT / "internal" / "gui" / f"{STEM}.ico")
    print(f"Validated ICO: {', '.join(map(str, SIZES))} px; all frames 32-bit RGBA")

    environment = os.environ.copy()
    environment["SOURCE_DATE_EPOCH"] = "0"
    for app in APPLICATIONS:
        directory = ROOT / "cmd" / app
        rc = directory / "icon_windows.rc"
        resource = directory / "icon_windows_amd64.syso"
        rc.write_text(f'1 ICON "{STEM}.ico"\n', encoding="ascii", newline="\n")
        command = [windres, "-I", "assets", "-F", "pe-x86-64", "-i", str(rc),
                   "-o", str(resource)]
        subprocess.run(command, cwd=ROOT, env=environment, check=True)
        validate_resource(resource)
        print(f"Validated amd64 Windows resource: {resource}")
    print(f"Generated SVG, 1024px PNG and ICO in {ASSETS}")
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (OSError, RuntimeError, subprocess.CalledProcessError) as error:
        print(f"error: {error}", file=sys.stderr)
        sys.exit(1)
