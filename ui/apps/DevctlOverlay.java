import com.devctl.ui.DevUI;
import com.devctl.ui.DevUI.GlyphFont;
import com.devctl.ui.DevUI.Layer;
import com.devctl.ui.DevUI.TouchInput;

import android.graphics.Canvas;

import java.io.BufferedReader;
import java.io.ByteArrayOutputStream;
import java.io.FileInputStream;
import java.io.InputStreamReader;

/**
 * DevctlOverlay — devctl 悬浮窗 (GG 修改器风格)
 *
 * 功能:
 *   - 悬浮球 (可拖动): GG 深色圆球, 点击打开全屏面板
 *   - 全屏覆盖窗口: 深色主题, 标题栏 + 信息区 (本机信息/连接状态)
 *   - 数据源: /data/local/devctl/dash.json (agent 刷写, Sprint 4 接通)
 *
 * 用法: app_process -Djava.class.path=/data/local/tmp/devctl/devui.dex /system/bin DevctlOverlay
 */
public class DevctlOverlay {

    // ---- 布局 (相对屏幕) ----
    static final int BALL = 150;
    static final int TITLE_H = 96;
    static final int PAD = 40;
    static final int TAP_PX = 30;

    // ---- GG 深色主题 ----
    static final int C_BG     = 0xFF0F1117;
    static final int C_TITLE  = 0xFF161A23;
    static final int C_ACCENT = 0xFF2A6DF3;
    static final int C_CARD   = 0xFF1A1F2B;
    static final int C_TEXT   = 0xFFE8ECF4;
    static final int C_SUB    = 0xFF8A93A6;
    static final int C_OK     = 0xFF3DDC84;
    static final int C_ERR    = 0xFFF25C5C;

    // ---- 状态机 ----
    static final int STATE_BALL = 0, STATE_PANEL = 1;
    static final int MODE_IDLE = 0, MODE_DRAG_BALL = 1, MODE_BTN = 2, MODE_DRAG_TITLE = 3;
    static int state = STATE_BALL;
    static int mode = MODE_IDLE;
    static int btnIdx = -1;

    static int ballX, ballY;
    static float grabOffX, grabOffY;
    static int PANEL_W, PANEL_H;

    // 面板信息
    static String infoTitle = "", info1 = "", info2 = "", info3 = "", info4 = "";
    static String connState = "", peerList = "", agentState = "";
    static int BTN_Y_OFF = 150;
    static int BTN_H = 96;

    static Layer ball, panel;
    static GlyphFont font;
    static TouchInput touch;

    public static void main(String[] args) throws Exception {
        System.out.println("[devui] GG overlay starting");
        System.out.flush();

        try {
            System.load("/data/local/tmp/devctl/libdevui_hide.so");
            System.out.println("[devui] hide so loaded");
        } catch (Throwable t) {
            System.out.println("[devui] hide skip: " + t);
        }

        DevUI.init();
        font = DevUI.loadFont("/data/local/tmp/devctl/devfont.bin");
        if (!font.ok) {
            System.out.println("[devui] FATAL: 字库缺失");
            System.exit(2);
        }
        PANEL_W = DevUI.screenW();
        PANEL_H = DevUI.screenH();
        System.out.println("[devui] font ok, screen=" + PANEL_W + "x" + PANEL_H);

        ball = DevUI.layer("DevctlBall", BALL, BALL, 0x7FFFFFFF - 100);
        panel = DevUI.layer("DevctlPanel", PANEL_W, PANEL_H, 0x7FFFFFFF - 99);
        ballX = DevUI.screenW() - BALL - 40;
        ballY = DevUI.screenH() / 3;
        ball.move(ballX, ballY);
        reloadInfo();
        drawBall();
        System.out.println("[devui] layers ready");

        Runtime.getRuntime().addShutdownHook(new Thread(new Runnable() {
            public void run() {
                System.out.println("[devui] shutdown hook cleanup");
                System.out.flush();
                ball.remove();
                panel.remove();
            }
        }));

        touch = DevUI.touch();
        touch.setTapThreshold(TAP_PX);
        touch.setListener(new TouchInput.Listener() {
            public void onDown(int x, int y) { DevctlOverlay.onDown(x, y); }
            public void onMove(int x, int y) { DevctlOverlay.onMove(x, y); }
            public void onUp(int x, int y, boolean tap) { DevctlOverlay.onUp(x, y, tap); }
        });
        touch.start();

        DevUI.watchCmd("/data/local/tmp/devctl/cmd", new DevUI.CmdFile.Handler() {
            public void onCmd(String cmd) {
                if (cmd.equals("open")) { openPanel(); }
                else if (cmd.equals("close")) { closePanel(); }
                else if (cmd.equals("refresh")) { reloadInfo(); if (state == STATE_PANEL) drawPanel(); }
                else if (cmd.equals("quit")) { System.exit(0); }
            }
        });

        System.out.println("[devui] ready, ball at " + ballX + "," + ballY);

        // 主循环: 保活 + 面板数据周期刷新 (2s)
        while (true) {
            Thread.sleep(2000);
            if (state == STATE_PANEL) {
                reloadInfo();
                drawPanel();
            }
        }
    }

    // ==================== 绘制: 悬浮球 ====================

    static void drawBall() {
        Canvas c = ball.lock();
        if (c == null) return;
        DevUI.color(c, C_BG);
        DevUI.rect(c, C_ACCENT, 4, 4, BALL - 4, BALL - 4);
        font.drawText(c, "devctl", (BALL - font.textW("devctl", 0.62f)) / 2f,
                (BALL - 64 * 0.62f) / 2f + 6, 0.62f);
        ball.unlock(c);
    }

    // ==================== 绘制: 全屏面板 ====================

    static void drawPanel() {
        Canvas c = panel.lock();
        if (c == null) return;
        DevUI.color(c, C_BG);

        // 标题栏
        DevUI.rect(c, C_TITLE, 0, 0, PANEL_W, TITLE_H);
        font.drawText(c, infoTitle, PAD, (TITLE_H - 64 * 0.9f) / 2f, 0.9f);
        DevUI.rect(c, C_ACCENT, 0, TITLE_H, PANEL_W, TITLE_H + 4);

        // 卡片 1: 本机信息
        int y = TITLE_H + 30;
        y = drawCardTitle(c, y, "本机信息");
        y = drawLine(c, y, info1, C_TEXT, 0.62f);
        y = drawLine(c, y, info2, C_SUB, 0.58f);

        // 卡片 2: 被连接状态
        y += 20;
        y = drawCardTitle(c, y, "被连接状态");
        y = drawLine(c, y, connState, C_TEXT, 0.62f);
        y = drawLine(c, y, peerList, C_SUB, 0.56f);

        // 卡片 3: 自身通信
        y += 20;
        y = drawCardTitle(c, y, "自身通信状态");
        y = drawLine(c, y, agentState, C_OK, 0.62f);
        y = drawLine(c, y, info4, C_SUB, 0.52f);

        // 底部按钮: 刷新 / 关闭
        int bx1 = PAD, bx2 = PANEL_W / 2 + 10;
        int by = PANEL_H - BTN_Y_OFF - BTN_H;
        drawButton(c, bx1, by, PANEL_W / 2 - PAD - 10, "刷新", 0);
        drawButton(c, bx2, by, PANEL_W / 2 - PAD - 10, "关闭", 1);

        panel.unlock(c);
    }

    static int drawCardTitle(Canvas c, int y, String t) {
        DevUI.rect(c, C_ACCENT, PAD, y, PAD + 6, y + 40);
        font.drawText(c, t, PAD + 22, y, 0.78f);
        return y + 54;
    }

    static int drawLine(Canvas c, int y, String text, int color, float scale) {
        if (text == null || text.isEmpty()) return y;
        font.drawText(c, text, PAD, y, scale);
        return y + Math.round(64 * scale) + 8;
    }

    static void drawButton(Canvas c, int bx, int by, int bw, String label, int idx) {
        DevUI.rect(c, idx == btnIdx ? C_ACCENT : C_CARD, bx, by, bx + bw, by + BTN_H);
        font.drawTextInRect(c, label, bx, by, bx + bw, by + BTN_H, 0.78f);
    }

    // ==================== 信息加载 (dash.json, Sprint 4 接通) ====================

    static void reloadInfo() {
        String model = prop("ro.product.model");
        String brand = prop("ro.product.brand");
        String rel = prop("ro.build.version.release");
        String sdk = prop("ro.build.version.sdk");
        infoTitle = "devctl 仪表盘";
        info1 = (brand.isEmpty() ? "" : brand + " ") + model;
        info2 = "Android " + rel + "  SDK " + sdk;
        // dash.json (agent 写, Sprint 4 有真实数据; 现在为空态)
        String dash = readFile("/data/local/devctl/dash.json");
        if (dash != null && dash.contains("peers")) {
            int n = countField(dash, "name");
            connState = "活跃连接数: " + n;
            peerList = formatPeers(dash);
            agentState = "agent 通信: 正常";
            info4 = "数据时间: " + field(dash, "now");
        } else {
            connState = "活跃连接数: 等待 agent 数据";
            peerList = "";
            agentState = "agent 通信: 未同步";
            info4 = "";
        }
    }

    static int countField(String s, String key) {
        int n = 0, i = 0;
        String k = "\"" + key + "\"";
        while ((i = s.indexOf(k, i)) >= 0) { n++; i += k.length(); }
        return n;
    }

    static String field(String s, String key) {
        try {
            int i = s.indexOf("\"" + key + "\":");
            if (i < 0) return "?";
            i = s.indexOf(":", i) + 1;
            while (i < s.length() && (s.charAt(i) == ' ' || s.charAt(i) == '"')) i++;
            int j = i;
            while (j < s.length() && s.charAt(j) != '"' && s.charAt(j) != ',' && s.charAt(j) != '\n') j++;
            return s.substring(i, j);
        } catch (Exception e) { return "?"; }
    }

    static String formatPeers(String s) {
        try {
            int i = s.indexOf("\"name\":");
            if (i < 0) return "  (无)";
            i += 8;
            int e = s.indexOf("\"", i + 1);
            return "  > " + s.substring(i, e);
        } catch (Exception ex) { return "  (解析失败)"; }
    }

    // ==================== 工具 ====================

    static String prop(String k) {
        try {
            Process p = Runtime.getRuntime().exec(new String[]{"/system/bin/getprop", k});
            BufferedReader r = new BufferedReader(new InputStreamReader(p.getInputStream()));
            String s = r.readLine();
            p.waitFor();
            return s == null ? "" : s.trim();
        } catch (Exception e) { return ""; }
    }

    static String readFile(String path) {
        try {
            FileInputStream f = new FileInputStream(path);
            ByteArrayOutputStream b = new ByteArrayOutputStream();
            byte[] buf = new byte[4096];
            int n;
            while ((n = f.read(buf)) > 0) b.write(buf, 0, n);
            f.close();
            return new String(b.toByteArray(), "UTF-8");
        } catch (Exception e) { return null; }
    }

    // ==================== 面板开合 ====================

    static void openPanel() {
        if (state == STATE_PANEL) return;
        state = STATE_PANEL;
        reloadInfo();
        panel.move(0, 0);
        ball.hide();
        drawPanel();
        System.out.println("[devui] panel open (fullscreen)");
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
            if (state == STATE_BALL) {
                if (sx >= ballX && sx <= ballX + BALL && sy >= ballY && sy <= ballY + BALL) {
                    mode = MODE_DRAG_BALL;
                    grabOffX = sx - ballX;
                    grabOffY = sy - ballY;
                }
            } else {
                if (sy <= TITLE_H) {
                    mode = MODE_DRAG_TITLE;
                } else {
                    int by = PANEL_H - BTN_Y_OFF - BTN_H;
                    if (sy >= by && sy <= by + BTN_H) {
                        if (sx <= PANEL_W / 2) { mode = MODE_BTN; btnIdx = 0; }
                        else { mode = MODE_BTN; btnIdx = 1; }
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
            }
        } catch (Exception e) {}
    }

    static void onUp(int sx, int sy, boolean tap) {
        try {
            if (tap) {
                if (mode == MODE_DRAG_BALL && state == STATE_BALL) openPanel();
                else if (mode == MODE_BTN && state == STATE_PANEL) {
                    if (btnIdx == 0) { reloadInfo(); drawPanel(); }
                    else if (btnIdx == 1) closePanel();
                } else if (mode == MODE_DRAG_TITLE && state == STATE_PANEL) {
                    closePanel();
                }
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
