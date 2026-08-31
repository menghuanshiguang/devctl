#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""genfont.py — Python 版字库生成器 (本机用, 替代 build 机 AWT GenFont)
格式与 ui/tools/GenFont.java 一致: [UTF len(2B) + UTF8 chars] + N*512B (1bit, 行优先)
渲染: NotoSansCJK 52pt, 64x64 居中, alpha>96 阈值
用法: python3 genfont.py [输出路径] [字符集文件]
"""
import struct, sys, os
from PIL import Image, ImageDraw, ImageFont

CELL = 64
FONT = "/usr/share/fonts/noto/NotoSansCJK-Regular.ttc"
SIZE = 52

ASCII = "".join(chr(c) for c in range(32, 127))
BASE_CN = "面板刷新收起退出型号系统内存架构设备正在连接失败运行状态版本核点击拖动悬浮控制加载功耗温度网络代理日志管置线离关闭开始停止重启手机处器储电量信时间期共个十百千万兆杠中英文大小写数字"
NEW_CN = "仪表盘总览客户端被其他未正常异常通信授权覆盖全屏窗口修改器风格双击拖动边缘隐藏清晰近远空闲活跃最后命令来自时间戳自我状态同步频率多少值字节真实负载心跳轮询监视监听会话允许拒绝密钥握手无"

def build_chars(extra=""):
    seen = []
    for c in ASCII + BASE_CN + NEW_CN + extra:
        if c not in seen: seen.append(c)
    return "".join(seen)

def render(ch, font):
    img = Image.new("L", (CELL, CELL), 0)
    d = ImageDraw.Draw(img)
    if ch == " ":
        return img
    bbox = d.textbbox((0, 0), ch, font=font)
    w, h = bbox[2] - bbox[0], bbox[3] - bbox[1]
    x = (CELL - w) // 2 - bbox[0]
    y = (CELL - h) // 2 - bbox[1]
    d.text((x, y), ch, fill=255, font=font)
    return img

def pack(img):
    row = bytearray(CELL // 8 * CELL)
    for yy in range(CELL):
        bits = 0
        bi = yy * (CELL // 8)
        for xx in range(CELL):
            bits = (bits << 1) | (1 if img.getpixel((xx, yy)) > 96 else 0)
            if (xx & 7) == 7:
                row[bi] = bits; bi += 1; bits = 0
    return bytes(row)

def main():
    out = sys.argv[1] if len(sys.argv) > 1 else "/tmp/devfont_new.bin"
    chars = build_chars()
    font = ImageFont.truetype(FONT, SIZE, index=0)
    with open(out, "wb") as f:
        raw = chars.encode("utf-8")
        f.write(struct.pack(">H", len(raw)))
        f.write(raw)
        for ch in chars:
            f.write(pack(render(ch, font)))
    size = os.path.getsize(out)
    print(f"OK {out} size={size} chars={len(chars)}")
    with open("/tmp/devfont.chars.txt", "w") as f: f.write(chars)
    return chars

if __name__ == "__main__":
    main()
