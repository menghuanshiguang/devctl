#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""assert_shot.py — 截图断言 (自动化 UI 验证)
用法: python3 assert_shot.py <shot.png> --mode {auto|ball|panel|lockscreen}
输出: JSON (checks + verdict)
"""
import sys, json, argparse
from PIL import Image

def detect_lockscreen(img):
    w, h = img.size
    bottom = img.crop((0, int(h * 0.6), w, h))
    px = bottom.load()
    w2, h2 = bottom.size
    light = 0
    for y in range(50, h2 - 50, 40):
        for x in range(50, w2 - 50, 40):
            r, g, b = px[x, y][:3]
            if r > 180 and g > 180 and b > 180:
                light += 1
    return light >= 5

def detect_ball(img):
    w, h = img.size
    x1 = int(w * 0.70); x2 = int(w * 0.95)
    y1 = int(h * 0.30); y2 = int(h * 0.60)
    reg = img.crop((x1, y1, x2, y2))
    px = reg.load()
    w2, h2 = reg.size
    blue = 0
    for y in range(0, h2, 4):
        for x in range(0, w2, 4):
            r, g, b = px[x, y][:3]
            if r < 90 and 70 < g < 140 and b > 180:
                blue += 1
    return blue > 20

def detect_panel(img):
    w, h = img.size
    px = img.load()
    dark, total = 0, 0
    for y in range(0, h, 8):
        for x in range(0, w, 8):
            r, g, b = px[x, y][:3]
            total += 1
            if r < 40 and g < 45 and b < 55:
                dark += 1
    return (dark / total if total else 0) > 0.5

def main():
    p = argparse.ArgumentParser()
    p.add_argument("shot")
    p.add_argument("--mode", default="auto", choices=["auto", "ball", "panel", "lockscreen"])
    args = p.parse_args()
    img = Image.open(args.shot).convert("RGB")
    res = {"file": args.shot, "size": f"{img.size[0]}x{img.size[1]}", "checks": {}}
    res["checks"]["lockscreen"] = detect_lockscreen(img)
    if args.mode in ("auto", "ball"):
        res["checks"]["ball"] = detect_ball(img)
    if args.mode in ("auto", "panel"):
        res["checks"]["panel"] = detect_panel(img)
    ok = [k for k, v in res["checks"].items() if v]
    res["verdict"] = "PASS" if ok else "FAIL"
    print(json.dumps(res, ensure_ascii=False, indent=2))

if __name__ == "__main__":
    main()
