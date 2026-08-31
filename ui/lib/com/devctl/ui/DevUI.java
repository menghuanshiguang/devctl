package com.devctl.ui;

import android.graphics.Bitmap;
import android.graphics.Canvas;
import android.graphics.Rect;
import android.graphics.RectF;

import java.io.BufferedReader;
import java.io.DataInputStream;
import java.io.FileInputStream;
import java.io.FileOutputStream;
import java.io.InputStreamReader;
import java.lang.reflect.Constructor;
import java.lang.reflect.Method;
import java.util.ArrayList;
import java.util.regex.Matcher;
import java.util.regex.Pattern;

/**
 * DevUI: devctl 渲染层 UI 地基库 (app_process, 无 APK)
 *
 * 架构说明:
 *   - 显示: SurfaceControl 反射 (不经过 WindowManager, 直接合成到屏幕顶层)
 *   - 文字: 点阵字库 GlyphFont (build 机 AWT 预渲染 devfont.bin, 1bit/字,
 *           运行时解码为 Bitmap 后 drawBitmap —— 绕开 app_process 下
 *           Typeface/Skia 字体 native 断言崩溃, 该路子已实测稳定)
 *   - 触摸: TouchInput (root 读 /dev/input 的 getevent 流, 自带设备发现、
 *           raw->屏幕坐标缩放、断流重连; 事件回调给上层做 hit-test)
 *   - 控制: CmdFile (轮询控制文件, 供 devctl client 远程发指令: open/close/
 *           refresh/quit/move, 无需改 agent)
 *
 * 用法:
 *   DevUI.init();
 *   DevUI.GlyphFont f = DevUI.loadFont("/data/local/tmp/devui/devfont.bin");
 *   DevUI.Layer ball = DevUI.layer("Ball", 150, 150, 0x7FFFFFFF - 100);
 *   ball.move(100, 200);
 *   Canvas c = ball.lock(); DevUI.color(c, 0xE6226DF3); ball.unlock(c);
 *
 * 编译链见 ui/build.ps1; 详尽的设备端坑位(全踩过)见 ui/README.md。
 */
public class DevUI {

    // ==================== 反射初始化 ====================

    private static boolean inited = false;
    private static Class<?> scClass, builderCls, transCls, surfCls, canvasCls, rectCls, rectFCls;
    private static Constructor<?> buCtor, transCtor, surfCtor;
    private static Method mSetName, mSetBuf, mSetFmt, mBuild, mSetLayer, mSetPos, mApply, mShow;
    private static Method mLock, mUnlock, mDrawColor, mClip, mSave, mRestore, mDrawBitmap;
    private static Object trans;

    private static int screenW = 1216, screenH = 2640;
    private static int orientation = 0;   // 0=竖 1=横90 3=反横

    /** 反射初始化 (幂等) */
    public static void init() throws Exception {
        if (inited) return;
        scClass = Class.forName("android.view.SurfaceControl");
        builderCls = Class.forName("android.view.SurfaceControl$Builder");
        transCls = Class.forName("android.view.SurfaceControl$Transaction");
        surfCls = Class.forName("android.view.Surface");
        canvasCls = Class.forName("android.graphics.Canvas");
        rectCls = Class.forName("android.graphics.Rect");
        rectFCls = Class.forName("android.graphics.RectF");

        buCtor = builderCls.getDeclaredConstructor(); buCtor.setAccessible(true);
        transCtor = transCls.getDeclaredConstructor(); transCtor.setAccessible(true);
        surfCtor = surfCls.getDeclaredConstructor(scClass); surfCtor.setAccessible(true);

        mSetName = builderCls.getMethod("setName", String.class);
        mSetBuf = builderCls.getMethod("setBufferSize", int.class, int.class);
        mSetFmt = builderCls.getMethod("setFormat", int.class); mSetFmt.setAccessible(true);
        mBuild = builderCls.getMethod("build");
        mSetLayer = transCls.getMethod("setLayer", scClass, int.class); mSetLayer.setAccessible(true);
        mSetPos = transCls.getMethod("setPosition", scClass, float.class, float.class); mSetPos.setAccessible(true);
        mApply = transCls.getMethod("apply"); mApply.setAccessible(true);
        mShow = transCls.getMethod("show", scClass); mShow.setAccessible(true);

        mLock = surfCls.getMethod("lockCanvas", rectCls);
        mUnlock = surfCls.getMethod("unlockCanvasAndPost", canvasCls);
        mDrawColor = canvasCls.getMethod("drawColor", int.class);
        mClip = canvasCls.getMethod("clipRect", float.class, float.class, float.class, float.class);
        mSave = canvasCls.getMethod("save");
        mRestore = canvasCls.getMethod("restore");
        mDrawBitmap = canvasCls.getMethod("drawBitmap", Bitmap.class, Rect.class, RectF.class, android.graphics.Paint.class);

        trans = transCtor.newInstance();
        inited = true;
        screenSize();
    }

    public static int orientation() { return orientation; }
    public static int screenW() { return screenW; }
    public static int screenH() { return screenH; }

    /** 重新读取屏幕尺寸 (支持旋转检测: 每次调用重新执行 wm size) */
    public static void refreshScreenSize() { screenSize(); }

    private static void screenSize() {
        // 真实屏幕方向: mCurrentOrientation (0=竖, 1=横, 2=反竖, 3=反横) — 不随窗口抖动
        // 物理尺寸: dumpsys display 的 DisplayDeviceInfo "1216 x 2640"
        int orientation = 0;
        int physW = 0, physH = 0;
        try {
            Process p = Runtime.getRuntime().exec(new String[]{"/system/bin/sh", "-c",
                    "dumpsys display | grep -oE 'mCurrentOrientation=[0-9]+' | head -1"});
            BufferedReader r = new BufferedReader(new InputStreamReader(p.getInputStream()));
            String l = r.readLine();
            p.waitFor();
            if (l != null) {
                Matcher m = Pattern.compile("mCurrentOrientation=([0-9]+)").matcher(l);
                if (m.find()) { orientation = Integer.parseInt(m.group(1)); }
            }
        } catch (Exception e) {}
        try {
            Process p = Runtime.getRuntime().exec(new String[]{"/system/bin/sh", "-c",
                    "dumpsys display | grep -oE '[0-9]+ x [0-9]+' | head -1"});
            BufferedReader r = new BufferedReader(new InputStreamReader(p.getInputStream()));
            String l = r.readLine();
            p.waitFor();
            if (l != null) {
                Matcher m = Pattern.compile("([0-9]+) x ([0-9]+)").matcher(l);
                if (m.find()) { physW = Integer.parseInt(m.group(1)); physH = Integer.parseInt(m.group(2)); }
            }
        } catch (Exception e) {}
        // 兜底: wm size
        if (physW <= 0 || physH <= 0) {
            try {
                Process p = Runtime.getRuntime().exec(new String[]{"/system/bin/sh", "-c", "wm size"});
                BufferedReader r = new BufferedReader(new InputStreamReader(p.getInputStream()));
                String l;
                while ((l = r.readLine()) != null) {
                    Matcher m = Pattern.compile("(\\d+)\\s*x\\s*(\\d+)").matcher(l);
                    if (m.find()) { physW = Integer.parseInt(m.group(1)); physH = Integer.parseInt(m.group(2)); break; }
                }
                p.waitFor();
            } catch (Exception e) {}
        }
        // 按方向输出实际宽高: 横屏(1/3)交换宽高
        if (orientation == 1 || orientation == 3) {
            screenW = physH; screenH = physW;
        } else {
            screenW = physW; screenH = physH;
        }
    }

    // ==================== 绘制原语 (免 Paint, 全部绕开字体) ====================

    /** 全画布铺色 (受当前 clip 限制) */
    public static void color(Canvas c, int color) {
        try { mDrawColor.invoke(c, color); } catch (Exception e) {}
    }

    /** 实心矩形: clip 后铺色再还原 */
    public static void rect(Canvas c, int color, float x1, float y1, float x2, float y2) {
        try {
            mSave.invoke(c);
            mClip.invoke(c, x1, y1, x2, y2);
            mDrawColor.invoke(c, color);
            mRestore.invoke(c);
        } catch (Exception e) {}
    }

    /** 位图贴到 (x,y) 宽高 (w,h) */
    public static void bitmap(Canvas c, Bitmap b, float x, float y, float w, float h) {
        try {
            Rect src = new Rect(0, 0, b.getWidth(), b.getHeight());
            RectF dst = new RectF(x, y, x + w, y + h);
            mDrawBitmap.invoke(c, b, src, dst, null);
        } catch (Exception e) {}
    }

    public static void save(Canvas c) { try { mSave.invoke(c); } catch (Exception e) {} }
    public static void restore(Canvas c) { try { mRestore.invoke(c); } catch (Exception e) {} }
    public static void clip(Canvas c, float x1, float y1, float x2, float y2) {
        try { mClip.invoke(c, x1, y1, x2, y2); } catch (Exception e) {}
    }

    // ==================== 悬浮层 ====================

    /**
     * 创建悬浮层。z 越大越靠上 (参考: 0x7FFFFFFF-100)。
     * 创建后默认显示在屏幕外 (-5000), 用 move() 定位。
     */
    public static Layer layer(String name, int w, int h, int z) throws Exception {
        return new Layer(name, w, h, z);
    }

    /** 悬浮层: 一个 SurfaceControl + Surface, 位置可随时移动, 内容用 lock/unlock 绘制 */
    public static class Layer {
        final String name;
        final int w, h;
        Object sc, surface;
        float x, y;
        boolean removed = false;

        Layer(String name, int w, int h, int z) throws Exception {
            this.name = name;
            this.w = w;
            this.h = h;
            Object b = buCtor.newInstance();
            mSetName.invoke(b, name);
            mSetBuf.invoke(b, w, h);
            mSetFmt.invoke(b, 1);
            sc = mBuild.invoke(b);
            synchronized (trans) {
                mSetLayer.invoke(trans, sc, z);
                mSetPos.invoke(trans, sc, -5000f, -5000f);
                mShow.invoke(trans, sc);
                mApply.invoke(trans);
            }
            surface = surfCtor.newInstance(sc);
        }

        public int width() { return w; }
        public int height() { return h; }
        public float x() { return x; }
        public float y() { return y; }

        /** 立即移动 (线程安全) */
        public void move(float nx, float ny) {
            if (removed) return;
            x = nx; y = ny;
            synchronized (trans) {
                try {
                    mSetPos.invoke(trans, sc, nx, ny);
                    mApply.invoke(trans);
                } catch (Exception e) {}
            }
        }

        /** 移出屏幕 (视觉隐藏, 层保留) */
        public void hide() { move(-5000f, -5000f); }

        /** 锁画布绘制 (全尺寸), 用完必须 unlock() */
        public Canvas lock() {
            try {
                Object r = rectCls.getDeclaredConstructor(int.class, int.class, int.class, int.class)
                        .newInstance(0, 0, w, h);
                Object c = mLock.invoke(surface, r);
                return c == null ? null : (Canvas) c;
            } catch (Exception e) { return null; }
        }

        public void unlock(Canvas c) {
            if (c == null) return;
            try { mUnlock.invoke(surface, c); } catch (Exception e) {}
        }

        /** 彻底移除层 */
        public void remove() {
            if (removed) return;
            removed = true;
            try {
                Method mRemove = transCls.getMethod("remove", scClass);
                mRemove.setAccessible(true);
                synchronized (trans) {
                    mRemove.invoke(trans, sc);
                    mApply.invoke(trans);
                }
            } catch (Exception e) {}
        }
    }

    // ==================== 点阵字库 ====================

    /**
     * 加载 devfont.bin (格式: [UTF len 2B][UTF8 字符串][每字符 64*64/8 字节 1bit, 行优先])
     * 生成器: ui/tools/GenFont.java (build 机跑)
     */
    public static GlyphFont loadFont(String path) { return new GlyphFont(path); }

    public static class GlyphFont {
        public final boolean ok;
        private String chars = "";
        private ArrayList<Bitmap> glyphs = new ArrayList<Bitmap>();
        private static final int CELL = 64;

        GlyphFont(String path) {
            boolean loaded = false;
            try {
                DataInputStream in = new DataInputStream(new FileInputStream(path));
                int len = in.readUnsignedShort();
                byte[] buf = new byte[len];
                in.readFully(buf);
                chars = new String(buf, "UTF-8");
                int cellBytes = CELL * CELL / 8;
                byte[] cell = new byte[cellBytes];
                for (int i = 0; i < chars.length(); i++) {
                    in.readFully(cell);
                    int[] px = new int[CELL * CELL];
                    for (int y = 0; y < CELL; y++) {
                        for (int x = 0; x < CELL; x++) {
                            int bit = (cell[y * 8 + x / 8] >> (7 - (x & 7))) & 1;
                            px[y * CELL + x] = bit == 1 ? 0xFFFFFFFF : 0x00000000;
                        }
                    }
                    Bitmap bm = Bitmap.createBitmap(CELL, CELL, Bitmap.Config.ARGB_8888);
                    bm.setPixels(px, 0, CELL, 0, 0, CELL, CELL);
                    glyphs.add(bm);
                }
                in.close();
                loaded = true;
            } catch (Exception e) {}
            ok = loaded;
        }

        public Bitmap glyph(char c) {
            if (!ok) return null;
            int idx = chars.indexOf(c);
            return idx >= 0 ? glyphs.get(idx) : null;
        }

        /** 文本像素宽 (scale = CELL 的缩放, 空格按 34*scale) */
        public float textW(String s, float scale) {
            float w = 0;
            for (int i = 0; i < s.length(); i++)
                w += (s.charAt(i) == ' ' ? 34 : CELL) * scale;
            return w;
        }

        /** 在 (x,y) 处绘制文本 (左上角锚点) */
        public void drawText(Canvas c, String s, float x, float y, float scale) {
            float dx = x;
            for (int i = 0; i < s.length(); i++) {
                char ch = s.charAt(i);
                if (ch == ' ') { dx += 34 * scale; continue; }
                Bitmap g = glyph(ch);
                if (g != null) bitmap(c, g, dx, y, CELL * scale, CELL * scale);
                dx += CELL * scale;
            }
        }

        /** 水平居中绘制 (cx = 中心 x, y = 顶) */
        public void drawTextCentered(Canvas c, String s, float cx, float y, float scale) {
            drawText(c, s, cx - textW(s, scale) / 2f, y, scale);
        }

        /** 按钮常用: 在矩形区域内居中 */
        public void drawTextInRect(Canvas c, String s, float x1, float y1, float x2, float y2, float scale) {
            drawText(c, s, x1 + (x2 - x1 - textW(s, scale)) / 2f,
                    y1 + (y2 - y1 - CELL * scale) / 2f, scale);
        }
    }

    // ==================== 触摸输入 (getevent 流) ====================

    /**
     * 触摸输入: 自动发现触摸设备 (getevent -pl), 流式解析 (getevent -lt),
     * raw 坐标按设备 max 值线性缩放到屏幕像素, 断流自动重连。
     * 回调均在后台线程执行, 上层自行同步。
     */
    public static TouchInput touch() { return new TouchInput(); }

    public static class TouchInput {
        public interface Listener {
            void onDown(int x, int y);
            void onMove(int x, int y);
            /** tap = 按下到抬起位移小于阈值 */
            void onUp(int x, int y, boolean tap);
        }

        private volatile Listener listener;
        private volatile boolean running = false;
        private Thread thread;
        private int tapThreshold = 30;

        public void setListener(Listener l) { this.listener = l; }
        public void setTapThreshold(int px) { this.tapThreshold = px; }

        /** 启动后台线程 (阻塞读事件, 自动重连) */
        public void start() {
            if (running) return;
            running = true;
            thread = new Thread(new Runnable() { public void run() { loop(); } });
            thread.setDaemon(true);
            thread.start();
        }

        public void stop() {
            running = false;
            if (thread != null) thread.interrupt();
        }

        // ---- 内部状态 ----
        private String devPath = null;
        private float maxX = 1, maxY = 1;
        private boolean down = false;
        private float lastRx, lastRy;
        private float downX, downY;

        private void loop() {
            while (running) {
                try {
                    if (!findDevice()) { Thread.sleep(3000); continue; }
                    stream();
                    Thread.sleep(2000);
                } catch (Exception e) { try { Thread.sleep(1000); } catch (Exception ie) {} }
            }
        }

        private boolean findDevice() {
            if (devPath != null && maxX > 1) return true;
            try {
                Process p = Runtime.getRuntime().exec(new String[]{"/system/bin/getevent", "-pl"});
                BufferedReader r = new BufferedReader(new InputStreamReader(p.getInputStream()));
                String l, curDev = null, devX = null, devY = null;
                float mx = 0, my = 0;
                Pattern pMax = Pattern.compile("value (\\d+), min (\\d+), max (\\d+)");
                while ((l = r.readLine()) != null) {
                    if (l.startsWith("add device")) {
                        if (devX != null && devY != null) break;
                        int i = l.indexOf("/dev/input/");
                        if (i >= 0) { curDev = l.substring(i).trim(); devX = null; devY = null; }
                        continue;
                    }
                    if (curDev == null) continue;
                    String t = l.trim();
                    if (t.startsWith("ABS_MT_POSITION_X")) {
                        Matcher m = pMax.matcher(l);
                        if (m.find()) { devX = curDev; mx = Float.parseFloat(m.group(3)); }
                    } else if (t.startsWith("ABS_MT_POSITION_Y")) {
                        Matcher m = pMax.matcher(l);
                        if (m.find()) { devY = curDev; my = Float.parseFloat(m.group(3)); }
                    }
                }
                try { p.waitFor(); } catch (Exception e) {}
                if (devX != null && devY != null) {
                    devPath = devX; maxX = mx; maxY = my;
                    return true;
                }
            } catch (Exception e) {}
            return false;
        }

        private void stream() {
            try {
                // 用无 -lt 的 getevent (ColorOS/oplus 驱动在 -lt 模式下不上报位置事件!)
                // 输出格式: "0003 0035 00002e00" (type code value, 十六进制)
                Process p = Runtime.getRuntime().exec(new String[]{"/system/bin/getevent", devPath});
                BufferedReader r = new BufferedReader(new InputStreamReader(p.getInputStream()));
                String l;
                while (running && (l = r.readLine()) != null) {
                    String[] tk = l.trim().split("\\s+");
                    // tk = [type, code, value] (如 ["0003","0035","00002e00"])
                    if (tk.length < 3) continue;
                    try {
                        int type = (int) Long.parseLong(tk[0], 16);
                        int code = (int) Long.parseLong(tk[1], 16);
                        int val = (int) Long.parseLong(tk[2], 16);
                        if (type == 0x03) { // EV_ABS
                            if (code == 0x39) { // ABS_MT_TRACKING_ID
                                // UP = 0xFFFFFFFF (parse16 得正数 4294967295, 需专门判断)
                                if (val == 0xFFFFFFFF || val == -1) {
                                    if (down) { onUp(); down = false; }
                                } else if (val >= 0) {
                                    down = true;
                                }
                            } else if (code == 0x35) { lastRx = val; }  // ABS_MT_POSITION_X
                            else if (code == 0x36) { lastRy = val; }  // ABS_MT_POSITION_Y
                        } else if (type == 0x00) { // EV_SYN
                            onFrame();
                        }
                    } catch (Exception e) {}
                }
                p.destroy();
            } catch (Exception e) {}
        }

        /**
         * 触摸坐标 → 屏幕坐标 (按屏幕方向旋转)。
         * 触摸设备 raw 坐标始终是物理竖屏方向 (raw X=物理宽, raw Y=物理高),
         * 屏幕旋转时需把 raw 坐标映射旋转:
         *   方向0(竖):  sx = rx * W/maxX,      sy = ry * H/maxY
         *   方向1(横90): sx = ry * W/maxY,     sy = (maxX - rx) * H/maxX
         *   方向3(反横): sx = (maxY - ry) * W/maxY, sy = rx * H/maxX
         */
        private int sx() {
            float W = DevUI.screenW(), H = DevUI.screenH();
            int o = DevUI.orientation;
            if (o == 1) return Math.round(lastRy * W / maxY);
            if (o == 3) return Math.round((maxY - lastRy) * W / maxY);
            return Math.round(lastRx * W / maxX);
        }
        private int sy() {
            float W = DevUI.screenW(), H = DevUI.screenH();
            int o = DevUI.orientation;
            if (o == 1) return Math.round((maxX - lastRx) * H / maxX);
            if (o == 3) return Math.round(lastRx * H / maxX);
            return Math.round(lastRy * H / maxY);
        }

        private void onFrame() {
            if (!down) return;
            Listener l = listener;
            if (l == null) return;
            int x = sx(), y = sy();
            if (downX == 0 && downY == 0) { downX = x; downY = y; }
            l.onDown(x, y);
            l.onMove(x, y);
        }

        private void onUp() {
            Listener l = listener;
            if (l == null) return;
            int x = sx(), y = sy();
            boolean tap = Math.abs(x - downX) + Math.abs(y - downY) <= tapThreshold;
            downX = downY = 0;
            l.onUp(x, y, tap);
        }
    }

    // ==================== 控制通道 (文件轮询) ====================

    /**
     * 控制通道: 后台线程轮询控制文件, 内容按行回调。
     * devctl client 侧只需 `echo open > /data/local/tmp/devui/cmd` 即可远程控制,
     * agent 零改动。识别到新内容后回调, 并清空文件。
     */
    public static void watchCmd(String path, final CmdFile.Handler h) {
        new Thread(new Runnable() {
            public void run() {
                long lastMod = -1;
                while (true) {
                    try {
                        java.io.File f = new java.io.File(path);
                        if (f.exists() && f.length() > 0 && f.lastModified() != lastMod) {
                            lastMod = f.lastModified();
                            String content = readAll(f);
                            try { f.delete(); } catch (Exception e) {}
                            if (content != null) {
                                String[] lines = content.split("\n");
                                for (String line : lines) {
                                    String cmd = line.trim();
                                    if (cmd.length() > 0) h.onCmd(cmd);
                                }
                            }
                        }
                    } catch (Exception e) {}
                    try { Thread.sleep(300); } catch (Exception ie) { return; }
                }
            }
        }).start();
    }

    public interface CmdFile { interface Handler { void onCmd(String cmd); } }

    private static String readAll(java.io.File f) {
        try {
            FileInputStream in = new FileInputStream(f);
            byte[] buf = new byte[(int) f.length()];
            int n = 0, r;
            while (n < buf.length && (r = in.read(buf, n, buf.length - n)) > 0) n += r;
            in.close();
            return new String(buf, 0, n, "UTF-8");
        } catch (Exception e) { return null; }
    }
}
