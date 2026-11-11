#!/usr/bin/env python3
"""从桌面阅读器共享 SVG 生成 Windows ICO；需要 CairoSVG 和 Pillow。"""

from io import BytesIO
from pathlib import Path
import struct


def generate_icon(output_path=None):
    import cairosvg
    from PIL import Image

    directory = Path(__file__).resolve().parent
    source = directory.parents[1] / "ofd-viewer/share/io.github.zc310.ofd.svg"
    svg = source.read_bytes()
    frames = []
    # 小尺寸使用传统 DIB，兼容 NSIS；256px 使用 PNG，保留高 DPI 清晰度及透明背景。
    for size in [16, 24, 32, 48, 64, 128, 256]:
        # 各尺寸独立超采样，保留小图标的字母轮廓和圆角抗锯齿。
        raster = cairosvg.svg2png(bytestring=svg, output_width=size * 4, output_height=size * 4)
        image = Image.open(BytesIO(raster)).convert("RGBA")
        frame = image.resize((size, size), Image.Resampling.LANCZOS)
        data = BytesIO()
        if size == 256:
            frame.save(data, format="PNG")
            payload = data.getvalue()
        else:
            frame.save(data, format="ICO", sizes=[(size, size)], bitmap_format="bmp")
            raw = data.getvalue()
            length, offset = struct.unpack_from("<II", raw, 14)
            payload = raw[offset:offset + length]
        frames.append((size, payload))
    output = bytearray(struct.pack("<HHH", 0, 1, len(frames)))
    offset = 6 + 16 * len(frames)
    for size, payload in frames:
        output.extend(struct.pack("<BBBBHHII", size % 256, size % 256, 0, 0, 1, 32, len(payload), offset))
        offset += len(payload)
    for _, payload in frames:
        output.extend(payload)
    (directory / "icon.ico" if output_path is None else Path(output_path)).write_bytes(output)


if __name__ == "__main__":
    generate_icon()
