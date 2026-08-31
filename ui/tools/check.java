import java.io.BufferedReader;
import java.io.DataInputStream;
import java.io.File;
import java.io.FileInputStream;
import java.io.InputStreamReader;
import java.lang.reflect.Constructor;
import java.lang.reflect.Method;
import java.util.ArrayList;
import java.util.regex.Matcher;
import java.util.regex.Pattern;

import android.graphics.Bitmap;
import android.graphics.Canvas;
import android.graphics.Color;
import android.graphics.Rect;
import android.graphics.RectF;

/**
 * DevctlOverlay: 可拖动的悬浮控件 + 点击进入二级面板 (app_process 版, 无 APK)
 *
 * 显示: SurfaceControl 反射 (同 DevctlUI 色块方案)
 * 触摸: root 直接读 /dev/input (getevent 流), 自己 hit-test:
 *       手指落在球上 → 拖动移动 / 抬手未动 = 点击 → 展开二级面板
 * 文字: 点阵字库 devfont.bin (build 机 AWT 预渲染, 1bit/字), 避免
 *       app_process 下 Typeface native 崩溃 → 全部走 drawBitmap
 *
 * 用法: app_process -Djava.class.path=/xxx.dex /system/bin DevctlOverlay [seconds] [auto]
 *       auto: 启动 5s 后自动展开面板 (截图验证用)
 * 字库: 必须先行放在 /data/local/tmp/devfont.bin
 */
public class DevctlOverlay {

    // ---------- 布局 (像素, 屏幕 1216x2640 基准) ----------
    static final int BALL = 150;
    static final int PANEL_W = 560, PANEL_H = 640;
    static final int TITLE_H = 84;
    static final int BTN_Y = 480, BTN_H = 110;
    static final int[] BTN_X = {30, 205, 380};
    static final int BTN_W = 150;
    static final int FONT_PATH = 32;              // 字库路径
    static final String FONT_FILE = "/data/local/tmp/devfont.bin";

    // 状态
    static int screenW = 1216, screenH = 2640;
    static int ballX, ballY;                       // 球左上角
    static int panelX, panelY;                     // 面板左上角
    static final int STATE_BALL = 0, STATE_PANEL = 1;
    static int state = STATE_BALL;
    static final int MODE_IDLE = 0, MODE_DRAG_BALL = 1, MODE_DRAG_PANEL = 2, MODE_BTN = 3;
    static int mode = MODE_IDLE;

    // 拖动数据
    static float grabOffX, grabOffY;
    static float downRawX, downRawY;
    static float downSX, downSY;
    static int btnIdx = -1;
    static float lastRawX, lastRawY;

    // 触摸设备
    static String touchDev = null;
    static float tMaxX = 1, tMaxY = 1;

    // 面板信息
    static String info1 = "", info2 = "", info3 = "", info4 = "";

    // 反射句柄
    static Object scBall, scPanel, transObj;
    static Object surfaceBall, surfacePanel;
    static Class<?> canvasCls, rectCls, rectFCls;
    static Method mLock, mUnlock, mApply, mSetPos, mDrawColor, mClip, mSave, mRestore, mDrawBitmap;
    static Canvas canvasBall, canvasPanel;         // 缓存 Surface 的画布锁
    static DevFont font;

    static boolean autoMode = false;

    static void log(String s) { System.out.println("[ov] " + s); System.out.flush(); }

    public static void main(String[] args) throws Exception {
        int seconds = 0;
        if (args.length > 0) seconds = Integer.parseInt(args[0]);
        if (args.length > 1 && args[1].equals("auto")) autoMode = true;

        System.out.println("[ov] devctl overlay starting...");
        System.out.flush();
        log("s0 pre-screen");
        screenSize();
        log("s1 screen=" + screenW + "x" + screenH);
        initSurface();
        log("s2 surface");
        font = new DevFont(FONT_FILE);
        log("s3 font=" + (font != null ? font.ok : "null"));

        // 信息采集
        reloadInfo();
        log("s4 info: " + info1 + " | " + info2 + " | " + info3 + " | " + info4);

        // 初始球位置 (右侧中部)
        ballX = screenW - BALL - 40;
        ballY = screenH / 3;
        drawBall();
        log("s5 drawn");

        // 面板 surface 建好后先移出屏幕
        move(scPanel, -5000, -5000);
        log("s6 panel moved");

        System.out.println("[ov] created, screen=" + screenW + "x" + screenH +
                " touchdev=" + (touchDev != null && findTouchDevice() ? touchDev : "(扫描中)") +
                " font=" + (font.ok ? "ok" : "MISSING"));
        if (!font.ok) {
            System.out.println("[ov] FATAL: 字库缺失, 退出");
            System.exit(2);
        }

        // 触摸线程
        Thread t = new Thread(new Runnable() { public void run() { touchLoop(); } });
        t.setDaemon(true);
        t.start();

        // auto 模式: 5s 后自动展开面板 (验证)
        if (autoMode) {
            Thread.sleep(5000);
            System.out.println("[ov] auto 展开面板");
            openPanel();
        }

        long t0 = System.currentTimeMillis();
        while (seconds <= 0 || System.currentTimeMillis() - t0 < seconds * 1000L) {
            Thread.sleep(1000);
        }
        cleanup();
        System.out.println("[ov] exited");
    }

    // ================= 信息采集 =================

    static void reloadInfo() {
        String model = prop("ro.product.model");
        String brand = prop("ro.product.brand");
        String rel = prop("ro.build.version.release");
        String sdk = prop("ro.build.version.sdk");
        info1 = (brand.isEmpty() ? "" : brand + " ") + model;
        info2 = "Android " + rel + " SDK " + sdk;
        info3 = memCpu();
        info4 = "devctl overlay v1";
    }

    static String prop(String k) {
        try {
            Process p = Runtime.getRuntime().exec(new String[]{"/system/bin/getprop", k});
            BufferedReader r = new BufferedReader(new InputStreamReader(p.getInputStream()));
            String s = r.readLine();
            p.waitFor();
            return s == null ? "" : s.trim();
        } catch (Exception e) { return ""; }
    }

    static String memCpu() {
        String mem = "";
        String cpu = "";
        try {
            BufferedReader r = new BufferedReader(new InputStreamReader(new FileInputStream("/proc/meminfo")));
            String l = r.readLine(); r.close();
            if (l != null) {
                Matcher m = Pattern.compile("(\\d+)").matcher(l);
                if (m.find()) {
                    long kb = Long.parseLong(m.group(1));
                    mem = String.format("%.1fG", kb / 1024.0 / 1024.0);
                }
            }
        } catch (Exception e) {}
        try {
            BufferedReader r = new BufferedReader(new InputStreamReader(new FileInputStream("/proc/cpuinfo")));
            String l; int n = 0;
            while ((l = r.readLine()) != null) if (l.startsWith("processor")) n++;
            r.close();
            cpu = n + " \u6838"; // 核
        } catch (Exception e) {}
        return "内存 " + mem + "  " + cpu;
    }

    // ================= 屏幕尺寸 =================

    static void screenSize() {
        try {
            Process p = Runtime.getRuntime().exec(new String[]{"/system/bin/sh", "-c", "wm size"});
            BufferedReader r = new BufferedReader(new InputStreamReader(p.getInputStream()));
            String l;
            while ((l = r.readLine()) != null) {
                Matcher m = Pattern.compile("(\\d+)\\s*x\\s*(\\d+)").matcher(l);
                if (m.find()) { screenW = Integer.parseInt(m.group(1)); screenH = Integer.parseInt(m.group(2)); break; }
            }
            p.waitFor();
        } catch (Exception e) {}
    }

    // ================= Surface 初始化 (同 DevctlUI) =================

    static void initSurface() throws Exception {
        Class<?> scClass = Class.forName("android.view.SurfaceControl");
        Class<?> builderCls = Class.forName("android.view.SurfaceControl$Builder");
        Class<?> transCls = Class.forName("android.view.SurfaceControl$Transaction");
        Class<?> surfCls = Class.forName("android.view.Surface");
        canvasCls = Class.forName("android.graphics.Canvas");
        rectCls = Class.forName("android.graphics.Rect");
        rectFCls = Class.forName("android.graphics.RectF");

        Constructor<?> bu = builderCls.getDeclaredConstructor();
        bu.setAccessible(true);

        // 球
        Object b = bu.newInstance();
        builderCls.getMethod("setName", String.class).invoke(b, "DevctlBall");
        builderCls.getMethod("setBufferSize", int.class, int.class).invoke(b, BALL, BALL);
        Method mFmt = builderCls.getMethod("setFormat", int.class); mFmt.setAccessible(true);
        mFmt.invoke(b, 1);
        scBall = builderCls.getMethod("build").invoke(b);

        // 面板
        Object b2 = bu.newInstance();
        builderCls.getMethod("setName", String.class).invoke(b2, "DevctlPanel");
        builderCls.getMethod("setBufferSize", int.class, int.class).invoke(b2, PANEL_W, PANEL_H);
        mFmt.invoke(b2, 1);
        scPanel = builderCls.getMethod("build").invoke(b2);

        Constructor<?> tc = transCls.getDeclaredConstructor();
        tc.setAccessible(true);
        transObj = tc.newInstance();
        Method mSetLayer = transCls.getMethod("setLayer", scClass, int.class);
        mSetPos = transCls.getMethod("setPosition", scClass, float.class, float.class);
        mApply = transCls.getMethod("apply");
        Method mShow = transCls.getMethod("show", scClass);
        mSetLayer.setAccessible(true); mSetPos.setAccessible(true);
        mApply.setAccessible(true); mShow.setAccessible(true);

        mSetLayer.invoke(transObj, scBall, Integer.MAX_VALUE - 100);
        mSetPos.invoke(transObj, scBall, 24f, 240f);
        mShow.invoke(transObj, scBall);
        mSetLayer.invoke(transObj, scPanel, Integer.MAX_VALUE - 99);
        mSetPos.invoke(transObj, scPanel, -5000f, -5000f);
        mShow.invoke(transObj, scPanel);
        mApply.invoke(transObj);

        Constructor<?> sctor = surfCls.getDeclaredConstructor(scClass);
        sctor.setAccessible(true);
        surfaceBall = sctor.newInstance(scBall);
        surfacePanel = sctor.newInstance(scPanel);

        mLock = surfCls.getMethod("lockCanvas", rectCls);
        mUnlock = surfCls.getMethod("unlockCanvasAndPost", canvasCls);

        // Canvas 绘制辅助
        mDrawColor = canvasCls.getMethod("drawColor", int.class);
        mClip = canvasCls.getMethod("clipRect", float.class, float.class, float.class, float.class);
        mSave = canvasCls.getMethod("save");
        mRestore = canvasCls.getMethod("restore");
        mDrawBitmap = canvasCls.getMethod("drawBitmap", Bitmap.class, Rect.class, RectF.class, android.graphics.Paint.class);
        System.out.println("[ov] surfaces created ok");
    }

    // lock 画布并返回 (全尺寸)
    static Canvas lock(Object surface, int w, int h) throws Exception {
        Object r = rectCls.getDeclaredConstructor(int.class, int.class, int.class, int.class)
                .newInstance(0, 0, w, h);
        Object c = mLock.invoke(surface, r);
        return c == null ? null : (Canvas) c;
    }

    static void post(Object surface, Canvas c) throws Exception {
        if (c != null) mUnlock.invoke(surface, c);
    }

    static void move(Object sc, float x, float y) throws Exception {
        mSetPos.invoke(transObj, sc, x, y);
        mApply.invoke(transObj);
    }

    // ================= 绘制 =================

    static void drawBall() {
        try {
            Canvas c = lock(surfaceBall, BALL, BALL);
            if (c == null) return;
            mDrawColor.invoke(c, 0xE6226DF3);
            // 中央画 "D" 字 (点阵)
            drawText(c, "D", (BALL - font.cellXY("D")) / 2f, (BALL - font.cellXY("D")) / 2f, 1.2f);
            post(surfaceBall, c);
        } catch (Exception e) { System.out.println("[ov] drawBall err: " + e); }
    }

    static void drawPanel() {
        try {
            Canvas c = lock(surfacePanel, PANEL_W, PANEL_H);
            if (c == null) return;
            // 主背景
            mDrawColor.invoke(c, 0xF2181A21);
            // 标题条
            mSave.invoke(c);
            mClip.invoke(c, 0f, 0f, (float) PANEL_W, (float) TITLE_H);
            mDrawColor.invoke(c, 0xFF23262F);
            mRestore.invoke(c);
            drawText(c, "devctl 面板", 36, 16, 0.85f);
            // 信息区
            drawText(c, info1, 36, 130, 0.72f);
            drawText(c, info2, 36, 212, 0.72f);
            drawText(c, info3, 36, 294, 0.72f);
            drawText(c, info4, 36, 376, 0.72f);
            // 按钮
            String[] names = {"刷新", "收起", "退出"};
            for (int i = 0; i < 3; i++) {
                int bx = BTN_X[i];
                mSave.invoke(c);
                mClip.invoke(c, (float) bx, (float) BTN_Y, (float) (bx + BTN_W), (float) (BTN_Y + BTN_H));
                mDrawColor.invoke(c, 0xFF2A3648);
                mRestore.invoke(c);
                float w = font.textW(names[i], 0.8f);
                drawText(c, names[i], bx + (BTN_W - w) / 2f, BTN_Y + (BTN_H - font.cellXY(names[i]) * 0.8f) / 2f, 0.8f);
            }
            post(surfacePanel, c);
        } catch (Exception e) { System.out.println("[ov] drawPanel err: " + e); }
    }

    // 点阵文本: 画到 canvas, scale 为 64px cell 的缩放
    static void drawText(Canvas c, String s, float x, float y, float scale) {
        try {
            float dx = x;
            for (int i = 0; i < s.length(); i++) {
                char ch = s.charAt(i);
                if (ch == ' ') { dx += 34 * scale; continue; }
                Bitmap g = font.glyph(ch);
                if (g != null) {
                    Rect src = new Rect(0, 0, 64, 64);
                    RectF dst = new RectF(dx, y, dx + 64 * scale, y + 64 * scale);
                    mDrawBitmap.invoke(c, g, src, dst, null);
                }
                dx += 64 * scale;
            }
        } catch (Exception e) { System.out.println("[ov] drawText err: " + e); }
    }

    // ================= 面板开合 =================

    static void openPanel() {
        reloadInfo();
        state = STATE_PANEL;
        // 面板从球附近展开 (钳制屏幕内)
        panelX = Math.max(20, Math.min(screenW - PANEL_W - 20, ballX + BALL - PANEL_W / 2));
        panelY = Math.max(80, Math.min(screenH - PANEL_H - 60, ballY - 100));
        try {
            move(scPanel, panelX, panelY);
            move(scBall, -5000, -5000);
        } catch (Exception e) { log("openPanel move err: " + e); }
        drawPanel();
        System.out.println("[ov] panel open at " + panelX + "," + panelY);
    }

    static void closePanel() {
        state = STATE_BALL;
        try {
            move(scBall, ballX, ballY);
            move(scPanel, -5000, -5000);
        } catch (Exception e) {}
        drawBall();
        System.out.println("[ov] panel closed");
    }

    static void quit() {
        System.out.println("[ov] exit via button");
        cleanup();
        System.exit(0);
    }

    static void cleanup() {
        try {
            java.lang.reflect.Method mRemove = transObj.getClass().getMethod("remove", Class.forName("android.view.SurfaceControl"));
            mRemove.setAccessible(true);
            mRemove.invoke(transObj, scBall);
            mRemove.invoke(transObj, scPanel);
            mApply.invoke(transObj);
        } catch (Exception e) {}
    }

    // ================= 触摸 =================

    static boolean findTouchDevice() {
        if (touchDev != null && tMaxX > 1) return true;
        try {
            Process p = Runtime.getRuntime().exec(new String[]{"/system/bin/getevent", "-pl"});
            BufferedReader r = new BufferedReader(new InputStreamReader(p.getInputStream()));
            String l;
            String curDev = null;
            String devX = null, devY = null;
            float mx = 0, my = 0;
            Pattern pMax = Pattern.compile("value (\\d+), min (\\d+), max (\\d+)");
            while ((l = r.readLine()) != null) {
                if (l.startsWith("add device")) {
                    if (devX != null && devY != null) break; // 上一设备是触摸
                    int i = l.indexOf("/dev/input/");
                    if (i >= 0) { curDev = l.substring(i).trim(); devX = null; devY = null; }
                    continue;
                }
                if (curDev == null) continue;
                if (l.trim().startsWith("ABS_MT_POSITION_X")) {
                    Matcher m = pMax.matcher(l);
                    if (m.find()) { devX = curDev; mx = Float.parseFloat(m.group(3)); }
                } else if (l.trim().startsWith("ABS_MT_POSITION_Y")) {
                    Matcher m = pMax.matcher(l);
                    if (m.find()) { devY = curDev; my = Float.parseFloat(m.group(3)); }
                }
            }
            try { p.waitFor(); } catch (Exception e) {}
            if (devX != null && devY != null) {
                touchDev = devX; tMaxX = mx; tMaxY = my;
                System.out.println("[ov] touch device " + touchDev + " max " + (int) mx + "x" + (int) my);
                return true;
            }
        } catch (Exception e) { System.out.println("[ov] findTouch err: " + e); }
        return false;
    }

    static void touchLoop() {
        while (true) {
            try {
                if (!findTouchDevice()) { Thread.sleep(3000); continue; }
                streamTouch();
                Thread.sleep(2000); // 流断了重试
            } catch (Exception e) { System.out.println("[ov] touch loop err: " + e); try { Thread.sleep(1000); } catch (Exception ie) {} }
        }
    }

    // getevent -lt 流解析
    static void streamTouch() {
        try {
            Process p = Runtime.getRuntime().exec(new String[]{"/system/bin/getevent", "-lt", touchDev});
            BufferedReader r = new BufferedReader(new InputStreamReader(p.getInputStream()));
            boolean down = false;   // 当前是否按下
            float rx = 0, ry = 0;   // 当前 raw 坐标
            String l;
            while ((l = r.readLine()) != null) {
                String[] tk = l.trim().split("\\s+");
                int ev = -1;
                for (int i = 0; i < tk.length; i++) {
                    if (tk[i].equals("EV_ABS")) { ev = i; break; }
                    if (tk[i].equals("EV_SYN")) { ev = -2; break; }
                }
                if (ev == -2) {
                    onFrame(down, rx, ry);
                    continue;
                }
                if (ev < 0) continue;
                try {
                    String code = tk[ev + 1];
                    int val = (int) Long.parseLong(tk[ev + 2], 16);
                    if (code.equals("ABS_MT_TRACKING_ID")) {
                        // <0 = up, >=0 = down
                        if (val < 0 && down) { onUp(); down = false; }
                        else if (val >= 0) { down = true; }
                    } else if (code.equals("ABS_MT_POSITION_X")) { rx = val; }
                    else if (code.equals("ABS_MT_POSITION_Y")) { ry = val; }
                } catch (Exception e) {}
            }
            p.destroy();
        } catch (Exception e) { System.out.println("[ov] stream err: " + e); }
    }

    // 每帧 (SYN) 处理
    static void onFrame(boolean down, float rx, float ry) {
        if (!down) return;
        lastRawX = rx; lastRawY = ry;
        float sx = rx * screenW / tMaxX;
        float sy = ry * screenH / tMaxY;
        try {
            if (mode == MODE_IDLE) {
                downRawX = rx; downRawY = ry;
                downSX = sx; downSY = sy;
                if (state == STATE_BALL) {
                    if (sx >= ballX && sx <= ballX + BALL && sy >= ballY && sy <= ballY + BALL) {
                        mode = MODE_DRAG_BALL;
                        grabOffX = sx - ballX;
                        grabOffY = sy - ballY;
                    }
                } else {
                    float px = sx - panelX, py = sy - panelY;
                    if (py >= 0 && py < TITLE_H && px >= 0 && px < PANEL_W) {
                        mode = MODE_DRAG_PANEL;
                        grabOffX = sx - panelX;
                        grabOffY = sy - panelY;
                        btnIdx = -1;
                    } else if (py >= BTN_Y && py <= BTN_Y + BTN_H) {
                        for (int i = 0; i < 3; i++) {
                            if (px >= BTN_X[i] && px <= BTN_X[i] + BTN_W) { mode = MODE_BTN; btnIdx = i; break; }
                        }
                    }
                }
            } else if (mode == MODE_DRAG_BALL) {
                // 拖动球
                ballX = (int) (sx - grabOffX);
                ballY = (int) (sy - grabOffY);
                ballX = Math.max(0, Math.min(screenW - BALL, ballX));
                ballY = Math.max(0, Math.min(screenH - BALL, ballY));
                move(scBall, ballX, ballY);
            } else if (mode == MODE_DRAG_PANEL) {
                panelX = (int) (sx - grabOffX);
                panelY = (int) (sy - grabOffY);
                panelX = Math.max(-PANEL_W + 100, Math.min(screenW - 100, panelX));
                panelY = Math.max(0, Math.min(screenH - 120, panelY));
                move(scPanel, panelX, panelY);
            }
        } catch (Exception e) {}
        // up 由 TRACKING_ID 负值直接调用 onUp
    }

    static void onUp() {
        float sx = lastRawX * screenW / tMaxX;
        float sy = lastRawY * screenH / tMaxY;
        float dx = sx - downSX, dy = sy - downSY;
        boolean moved = Math.abs(dx) + Math.abs(dy) > 30;
        if (!moved) {
            try {
                if (mode == MODE_DRAG_BALL && state == STATE_BALL) {
                    System.out.println("[ov] tap ball -> open panel");
                    openPanel();
                } else if (mode == MODE_BTN && state == STATE_PANEL && btnIdx >= 0) {
                    if (btnIdx == 0) { System.out.println("[ov] btn refresh"); reloadInfo(); drawPanel(); }
                    else if (btnIdx == 1) { System.out.println("[ov] btn close"); closePanel(); }
                    else if (btnIdx == 2) { System.out.println("[ov] btn exit"); quit(); }
                } else if (mode == MODE_DRAG_PANEL) {
                    // 标题栏轻点 = 收起
                    closePanel();
                }
            } catch (Exception e) {}
        }
        mode = MODE_IDLE;
        btnIdx = -1;
    }

    // 需要 in touch stream 里触发: TRACKING_ID 变负 → onUp
    static void touchUp() { onUp(); }

    // ================= 点阵字库 =================

    static class DevFont {
        String chars = "";
        ArrayList<Bitmap> glyphs = new ArrayList<Bitmap>();
        boolean ok = false;

        DevFont(String path) {
            try {
                DataInputStream in = new DataInputStream(new FileInputStream(path));
                int len = in.readUnsignedShort();
                byte[] buf = new byte[len];
                in.readFully(buf);
                chars = new String(buf, "UTF-8");
                int cellBytes = 64 * 64 / 8;
                byte[] cell = new byte[cellBytes];
                for (int i = 0; i < chars.length(); i++) {
                    in.readFully(cell);
                    int[] px = new int[64 * 64];
                    for (int y = 0; y < 64; y++) {
                        for (int x = 0; x < 64; x++) {
                            int bit = (cell[y * 8 + x / 8] >> (7 - (x & 7))) & 1;
                            px[y * 64 + x] = bit == 1 ? 0xFFFFFFFF : 0x00000000;
                        }
                    }
                    Bitmap bm = Bitmap.createBitmap(64, 64, Bitmap.Config.ARGB_8888);
                    bm.setPixels(px, 0, 64, 0, 0, 64, 64);
                    glyphs.add(bm);
                }
                in.close();
                ok = true;
                System.out.println("[ov] font loaded chars=" + chars.length());
            } catch (Exception e) {
                System.out.println("[ov] font load fail: " + e);
            }
        }

        Bitmap glyph(char c) {
            if (!ok) return null;
            int idx = chars.indexOf(c);
            return idx >= 0 ? glyphs.get(idx) : null;
        }

        float cellXY(String s) { return 64; }

        float textW(String s, float scale) {
            float w = 0;
            for (int i = 0; i < s.length(); i++)
                w += (s.charAt(i) == ' ' ? 34 : 64) * scale;
            return w;
        }
    }
}
