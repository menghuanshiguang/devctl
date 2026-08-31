import com.devctl.ui.DevUI;
import com.devctl.ui.DevUI.GlyphFont;
import com.devctl.ui.DevUI.Layer;
import com.devctl.ui.DevUI.TouchInput;

import android.graphics.Canvas;

import java.io.BufferedReader;
import java.io.FileInputStream;
import java.io.InputStreamReader;

/**
 * DevctlOverlay — devctl 悬浮控件应用 (基于 DevUI API 基座)
 *
 * 功能: 可拖动悬浮球 + 点击展开二级面板 (设备信息 + 刷新/收起/退出)
 * 形态: app_process 运行, 由 devctl agent 管理 (start/stop/status),
 *       进程自伪装 (libdevui_hide.so, 仅操作自身), 退出时 ShutdownHook 清理全部层。
 *
 * 控制通道: /data/local/tmp/devctl/cmd (CmdFile 轮询, client 发 open/close/refresh/quit)
 *
 * 用法: app_process -Djava.class.path=/data/local/tmp/devctl/devui.dex /system/bin DevctlOverlay
 */
public class DevctlOverlay {

    // ---- 布局 (px, 屏幕 1216x2640 基准) ----
    static final int BALL = 150;
    static final int PANEL_W = 640, PANEL_H = 640;
    static final int TITLE_H = 84;
    static final int BTN_Y = 480, BTN_H = 110;
    static final int[] BTN_X = {30, 235, 440};
    static final int BTN_W = 170;
    static final int TAP_PX = 30;

    // ---- 状态 ----
    static final int STATE_BALL = 0, STATE_PANEL = 1;
    static final int MODE_IDLE = 0, MODE_DRAG_BALL = 1, MODE_DRAG_PANEL = 2, MODE_BTN = 3;
    static int state = STATE_BALL;
    static int mode = MODE_IDLE;
    static int btnIdx = -1;

    static int ballX, ballY, panelX, panelY;
    static int downSX, downSY;
    static float grabOffX, grabOffY;

    static String info1 = "", info2 = "", info3 = "", info4 = "";

    static Layer ball, panel;
    static GlyphFont font;
    static TouchInput touch;

    public static void main(String[] args) throws Exception {
        System.out.println("[devui] starting");
        System.out.flush();

        // 0) 自伪装 (仅自身, 失败不致命)
        try {
            System.load("/data/local/tmp/devctl/libdevui_hide.so");
            System.out.println("[devui] hide so loaded");
        } catch (Throwable t) {
            System.out.println("[devui] hide skip: " + t);
        }

        // 1) API 基座
        DevUI.init();
        font = DevUI.loadFont("/data/local/tmp/devctl/devfont.bin");
        if (!font.ok) {
            System.out.println("[devui] FATAL: 字库缺失");
            System.exit(2);
        }
        System.out.println("[devui] font ok, screen=" + DevUI.screenW() + "x" + DevUI.screenH());

        // 2) 层
        ball = DevUI.layer("DevctlBall", BALL, BALL, 0x7FFFFFFF - 100);
        panel = DevUI.layer("DevctlPanel", PANEL_W, PANEL_H, 0x7FFFFFFF - 99);
        ballX = DevUI.screenW() - BALL - 42;
        ballY = DevUI.screenH() / 3;
        ball.move(ballX, ballY);
        reloadInfo();
        drawBall();
        System.out.println("[devui] layers ready");

        // 3) 退出的兜底清理
        Runtime.getRuntime().addShutdownHook(new Thread(new Runnable() {
            public void run() {
                System.out.println("[devui] shutdown hook cleanup");
                System.out.flush();
                ball.remove();
                panel.remove();
            }
        }));

        // 4) 触摸
        touch = DevUI.touch();
        touch.setTapThreshold(TAP_PX);
        touch.setListener(new TouchInput.Listener() {
            public void onDown(int x, int y) { DevctlOverlay.onDown(x, y); }
            public void onMove(int x, int y) { DevctlOverlay.onMove(x, y); }
            public void onUp(int x, int y, boolean tap) { DevctlOverlay.onUp(x, y, tap); }
        });
        touch.start();

        // 5) 控制通道 (client 远程命令)
        DevUI.watchCmd("/data/local/tmp/devctl/cmd", new DevUI.CmdFile.Handler() {
            public void onCmd(String cmd) {
                if (cmd.equals("open")) { openPanel(); }
                else if (cmd.equals("close")) { closePanel(); }
                else if (cmd.equals("refresh")) { reloadInfo(); if (state == STATE_PANEL) drawPanel(); }
                else if (cmd.equals("quit")) { System.exit(0); }
            }
        });

        System.out.println("[devui] ready, ball at " + ballX + "," + ballY);

        // 主线程保活
        while (true) Thread.sleep(1000);
    }

    // ==================== 绘制 ====================

    static void drawBall() {
        Canvas c = ball.lock();
        if (c == null) return;
        DevUI.color(c, 0xFFFFFFFF);
        DevUI.rect(c, 0xE6226DF3, 6, 6, BALL - 6, BALL - 6);
        font.drawText(c, "devctl", (BALL - font.textW("devctl", 0.62f)) / 2f, (BALL - 64 * 0.62f) / 2f + 6, 0.62f);
        ball.unlock(c);
    }

    static void drawPanel() {
        Canvas c = panel.lock();
        if (c == null) return;
        DevUI.color(c, 0xFFFFFFFF);
        DevUI.rect(c, 0xFF171A22, 4, 4, PANEL_W - 4, PANEL_H - 4);

        // 标题条
        DevUI.rect(c, 0xFF23262F, 4, 4, PANEL_W - 4, TITLE_H);
        font.drawText(c, "devctl 面板", 36, 16, 0.85f);

        // 信息区
        font.drawText(c, info1, 36, 130, 0.66f);
        font.drawText(c, info2, 36, 212, 0.66f);
        font.drawText(c, info3, 36, 294, 0.66f);
        font.drawText(c, info4, 36, 376, 0.66f);

        // 按钮
        String[] names = {"刷新", "收起", "退出"};
        for (int i = 0; i < 3; i++) {
            int bx = BTN_X[i];
            DevUI.rect(c, 0xFF2A3648, bx, BTN_Y, bx + BTN_W, BTN_Y + BTN_H);
            font.drawTextInRect(c, names[i], bx, BTN_Y, bx + BTN_W, BTN_Y + BTN_H, 0.8f);
        }
        panel.unlock(c);
    }

    // ==================== 信息 ====================

    static void reloadInfo() {
        String model = prop("ro.product.model");
        String brand = prop("ro.product.brand");
        String rel = prop("ro.build.version.release");
        String sdk = prop("ro.build.version.sdk");
        info1 = (brand.isEmpty() ? "" : brand + " ") + model;
        info2 = "Android " + rel + " SDK " + sdk;
        info3 = memCpu();
        info4 = "devctl devui v1";
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
        String mem = "", cpu = "";
        try {
            BufferedReader r = new BufferedReader(new InputStreamReader(new FileInputStream("/proc/meminfo")));
            String l = r.readLine(); r.close();
            java.util.regex.Matcher m = java.util.regex.Pattern.compile("(\\d+)").matcher(l == null ? "" : l);
            if (m.find()) mem = String.format("%.1fG", Long.parseLong(m.group(1)) / 1024.0 / 1024.0);
        } catch (Exception e) {}
        try {
            BufferedReader r = new BufferedReader(new InputStreamReader(new FileInputStream("/proc/cpuinfo")));
            String l; int n = 0;
            while ((l = r.readLine()) != null) if (l.startsWith("processor")) n++;
            r.close();
            cpu = n + " \u6838";
        } catch (Exception e) {}
        return "\u5185\u5b58 " + mem + "  " + cpu;
    }

    // ==================== 面板开合 ====================

    static void openPanel() {
        if (state == STATE_PANEL) return;
        state = STATE_PANEL;
        reloadInfo();
        panelX = Math.max(20, Math.min(DevUI.screenW() - PANEL_W - 20, ballX + BALL - PANEL_W / 2));
        panelY = Math.max(80, Math.min(DevUI.screenH() - PANEL_H - 60, ballY - 100));
        panel.move(panelX, panelY);
        ball.hide();
        drawPanel();
        System.out.println("[devui] panel open");
        System.out.flush();
    }

    static void closePanel() {
        if (state == STATE_BALL) return;
        state = STATE_BALL;
        panel.hide();
        ball.move(ballX, ballY);
        drawBall();
        System.out.println("[devui] panel closed");
        System.out.flush();
    }

    // ==================== 触摸状态机 ====================

    static void onDown(int sx, int sy) {
        try {
            if (mode != MODE_IDLE) return;
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
        } catch (Exception e) {}
    }

    static void onMove(int sx, int sy) {
        try {
            if (mode == MODE_DRAG_BALL) {
                ballX = clamp((int) (sx - grabOffX), 0, DevUI.screenW() - BALL);
                ballY = clamp((int) (sy - grabOffY), 0, DevUI.screenH() - BALL);
                ball.move(ballX, ballY);
            } else if (mode == MODE_DRAG_PANEL) {
                panelX = clamp((int) (sx - grabOffX), -PANEL_W + 100, DevUI.screenW() - 100);
                panelY = clamp((int) (sy - grabOffY), 0, DevUI.screenH() - 120);
                panel.move(panelX, panelY);
            }
        } catch (Exception e) {}
    }

    static void onUp(int sx, int sy, boolean tap) {
        try {
            if (tap) {
                if (mode == MODE_DRAG_BALL && state == STATE_BALL) openPanel();
                else if (mode == MODE_BTN && state == STATE_PANEL && btnIdx >= 0) {
                    if (btnIdx == 0) { reloadInfo(); drawPanel(); }
                    else if (btnIdx == 1) closePanel();
                    else if (btnIdx == 2) System.exit(0);
                } else if (mode == MODE_DRAG_PANEL) closePanel();
            }
        } catch (Exception e) {}
        mode = MODE_IDLE;
        btnIdx = -1;
    }

    static int clamp(int v, int lo, int hi) {
        if (v < lo) return lo;
        if (v > hi) return hi;
        return v;
    }
}
