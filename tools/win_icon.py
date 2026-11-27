#!/usr/bin/env python3
"""给 Windows exe 塞图标资源（不用 windres / rsrc，纯 pefile 手写 .rsrc 段）。

用法：python tools/win_icon.py windows/AntiSleep.exe

注意：图标块必须是 DIB（RT_ICON 不认 PNG 压缩的块），所以这里自己拼 ICO。
"""
import struct
import sys

import pefile
from PIL import Image

SIZES = (16, 24, 32, 48, 64, 128, 256)


def dib_bytes(img, size):
    """单个尺寸的 32 位 DIB（含 AND mask），用于 RT_ICON。"""
    size_ = img.resize((size, size), Image.LANCZOS)
    px = size_.load()
    rows = bytearray()
    for y in range(size - 1, -1, -1):  # bottom-up
        for x in range(size):
            r, g, b, a = px[x, y]
            rows += bytes((b, g, r, a))
    mask_row = b"\x00" * (((size + 31) // 32) * 4)
    mask = mask_row * size
    hdr = struct.pack(
        "<IiiHHIIiiII",
        40, size, size * 2, 1, 32, 0, size * size * 4 + len(mask),
        0, 0, 0, 0,
    )
    return hdr + bytes(rows) + mask


def build_ico(img):
    """生成 ICO 字节 + 各块的 (w,h,data)，宽度/高度字节按 ICO 规则（256 写 0）。"""
    blocks = []
    for s in SIZES:
        blocks.append((s, s, dib_bytes(img, s)))
    out = bytearray(struct.pack("<HHH", 0, 1, len(blocks)))
    dir_size = 6 + 16 * len(blocks)
    offset = dir_size
    for w, h, data in blocks:
        out += struct.pack(
            "<BBBBHHII",
            0 if w >= 256 else w, 0 if h >= 256 else h,
            0, 0, 1, 32, len(data), offset,
        )
        offset += len(data)
    for _, _, data in blocks:
        out += data
    return bytes(out), blocks


def group_icon_data(blocks):
    """GRPICONDIR：ICONDIRENTRY 最后是资源 id 而不是文件偏移。"""
    out = bytearray(struct.pack("<HHH", 0, 1, len(blocks)))
    for i, (w, h, data) in enumerate(blocks):
        out += struct.pack(
            "<BBBBHHIH",
            0 if w >= 256 else w, 0 if h >= 256 else h,
            0, 0, 1, 32, len(data), i + 1,
        )
    return bytes(out)


def resource_section(blocks):
    """返回 (段内容bytes, 每块数据的段内偏移列表, group 数据段内偏移)。"""
    n = len(blocks)
    off_root = 0
    off_dir3 = off_root + 32                 # root: 16 + 2 项
    off_dir14 = off_dir3 + 16 + 8 * n        # RT_ICON 目录
    off_lang3 = off_dir14 + 16 + 8           # RT_GROUP_ICON 目录（1 项）
    off_lang14 = off_lang3 + 24 * n
    off_dataent = off_lang14 + 24
    off_raw = off_dataent + 16 * (n + 1)     # data entry 每个 16 字节

    raw = bytearray()
    raw_off = []
    for _, _, data in blocks:
        pad = (-len(raw)) % 4
        raw += b"\x00" * pad
        raw_off.append(off_raw + len(raw))
        raw += data
    pad = (-len(raw)) % 4
    raw += b"\x00" * pad
    group_off = off_raw + len(raw)
    raw += group_icon_data(blocks)

    buf = bytearray()
    # 根目录：id 3 (ICON) / 14 (GROUP_ICON)
    buf += struct.pack("<IIHHHH", 0, 0, 0, 0, 0, 2)
    buf += struct.pack("<II", 3, off_dir3 | 0x80000000)
    buf += struct.pack("<II", 14, off_dir14 | 0x80000000)
    # RT_ICON 目录
    buf += struct.pack("<IIHHHH", 0, 0, 0, 0, 0, n)
    for i in range(n):
        buf += struct.pack("<II", i + 1, (off_lang3 + 24 * i) | 0x80000000)
    # RT_GROUP_ICON 目录
    buf += struct.pack("<IIHHHH", 0, 0, 0, 0, 0, 1)
    buf += struct.pack("<II", 1, off_lang14 | 0x80000000)
    # 每个图标的语言目录（LANG 用 0x0409 = en-US）
    for i in range(n):
        buf += struct.pack("<IIHHHH", 0, 0, 0, 0, 0, 1)
        buf += struct.pack("<II", 0x0409, off_dataent + 16 * i)
    # GROUP_ICON 的语言目录
    buf += struct.pack("<IIHHHH", 0, 0, 0, 0, 0, 1)
    buf += struct.pack("<II", 0x0409, off_dataent + 16 * n)
    # data entries（OffsetToData 稍后填 RVA）
    for i in range(n + 1):
        if i < n:
            buf += struct.pack("<IIII", 0, len(blocks[i][2]), 0, 0)
        else:
            buf += struct.pack("<IIII", 0, len(group_icon_data(blocks)), 0, 0)
    return bytes(buf), raw, raw_off, group_off, off_dataent


def _align(v, a):
    return (v + a - 1) // a * a


def inject(exe_path, png_path):
    import struct as st

    data = bytearray(open(exe_path, "rb").read())
    assert st.unpack_from("<H", data, 0)[0] == 0x5A4D, "不是 PE 文件"
    e = st.unpack_from("<I", data, 0x3C)[0]
    assert data[e:e + 4] == b"PE\0\0", "PE 签名不对"
    coff = e + 4
    nsec = st.unpack_from("<H", data, coff + 2)[0]
    optsz = st.unpack_from("<H", data, coff + 16)[0]
    opt = coff + 20
    magic = st.unpack_from("<H", data, opt)[0]
    soh = st.unpack_from("<I", data, opt + 60)[0]
    sa = st.unpack_from("<I", data, opt + 32)[0]
    fa = st.unpack_from("<I", data, opt + 36)[0]
    tbl = opt + optsz
    tbl_end = tbl + nsec * 40
    assert soh - tbl_end >= 40, "头部没有空间放下新段表项"

    # 已有段里不能叫 .rsrc
    names = [bytes(data[tbl + i * 40: tbl + i * 40 + 8]).rstrip(b"\x00") for i in range(nsec)]
    assert b".rsrc" not in names, "exe 已有 .rsrc 段"

    img = Image.open(png_path).convert("RGBA")
    _ico, blocks = build_ico(img)
    head, raw, raw_off, group_off, off_dataent = resource_section(blocks)
    vsize = len(head) + len(raw)
    blob = bytearray(head) + bytearray(vsize - len(head))
    for i in range(len(blocks) + 1):
        # RVA 稍后回填
        st.pack_into("<I", blob, off_dataent + 16 * i, 0)
    blob[vsize - len(raw):] = raw

    # 新段的 RVA 与文件偏移
    max_va = 0
    for i in range(nsec):
        o = tbl + i * 40
        va = st.unpack_from("<I", data, o + 12)[0]
        vs = st.unpack_from("<I", data, o + 8)[0]
        max_va = max(max_va, va + _align(vs, sa))
    new_va = _align(max_va, sa)
    for i in range(len(blocks) + 1):
        if i < len(blocks):
            st.pack_into("<I", blob, off_dataent + 16 * i, new_va + raw_off[i])
        else:
            st.pack_into("<I", blob, off_dataent + 16 * i, new_va + group_off)

    rawptr = _align(len(data), fa)
    rawsort = _align(vsize, fa)
    if rawptr > len(data):
        data += b"\x00" * (rawptr - len(data))
    data += bytes(blob)
    data += b"\x00" * (rawsort - len(blob))

    hdr = st.pack("<8sIIIIIIHHI",
                  b".rsrc", _align(vsize, sa) if False else vsize, new_va, rawsort, rawptr,
                  0, 0, 0, 0,
                  0x40000040)  # INITIALIZED_DATA | MEM_READ
    data[tbl_end:tbl_end + 40] = hdr
    st.pack_into("<H", data, coff + 2, nsec + 1)

    # SizeOfImage 与资源数据目录
    st.pack_into("<I", data, opt + 56, _align(new_va + vsize, sa))
    dd = opt + (112 if magic == 0x20B else 96)
    st.pack_into("<II", data, dd + 2 * 8, new_va, vsize)

    open(exe_path, "wb").write(bytes(data))
    print("icon ok: %d sizes, rsrc %d bytes @ RVA %#x" % (len(blocks), vsize, new_va))


if __name__ == "__main__":
    exe = sys.argv[1] if len(sys.argv) > 1 else "windows/AntiSleep.exe"
    png = sys.argv[2] if len(sys.argv) > 2 else "macOS/AppIcon.png"
    inject(exe, png)
