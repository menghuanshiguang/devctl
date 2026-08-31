package com.devctl.zapi;

import android.app.ActivityManager;
import android.content.Context;
import android.graphics.Color;
import android.graphics.PixelFormat;
import android.graphics.Typeface;
import android.os.Build;
import android.os.Handler;
import android.os.Looper;
import android.view.Gravity;
import android.view.MotionEvent;
import android.view.View;
import android.view.ViewGroup;
import android.view.WindowManager;
import android.widget.Button;
import android.widget.LinearLayout;
import android.widget.ScrollView;
import android.widget.TextView;

import org.json.JSONObject;

import java.io.BufferedReader;
import java.io.InputStreamReader;
import java.io.OutputStreamWriter;
import java.io.Writer;
import java.net.InetSocketAddress;
import java.security.SecureRandom;
import java.security.cert.X509Certificate;

import javax.net.ssl.SSLContext;
import javax.net.ssl.SSLSocket;
import javax.net.ssl.TrustManager;
import javax.net.ssl.X509TrustManager;

/**
 * 悬浮控件 (注入版): 可拖动悬浮球 + 点击进入二级面板
 * 寄生于宿主进程 (SystemUI), 全原生 View, 触摸由系统原生分发。
 */
public class InjectedOverlay {

    private static InjectedOverlay s;
    private static final Object LOCK = new Object();

    private Context ctx;
    private Handler main = new Handler(Looper.getMainLooper());
    private WindowManager wm;
    private View root;                     // 悬浮球
    private LinearLayout panel;            // 二级面板
    private WindowManager.LayoutParams lp;
    private TextView infoView;
    private boolean showingPanel = false;
    private String lastInfo = "";

    // ---- 拖动状态 ----
    private int downRawX, downRawY, downLpX, downLpY;
    private boolean dragging = false;

    public static void create(final Context ctx) {
        synchronized (LOCK) {
            if (s != null) return;
            final InjectedOverlay o = new InjectedOverlay(ctx);
            o.main.post(new Runnable() {
                @Override public void run() { o.build(); }
            });
            s = o;
        }
    }

    public static InjectedOverlay get() { return s; }

    public static void destroy() {
        synchronized (LOCK) {
            if (s == null) return;
            final InjectedOverlay o = s;
            s = null;
            o.main.post(new Runnable() {
                @Override public void run() { o.teardown(); }
            });
        }
    }

    private InjectedOverlay(Context ctx) { this.ctx = ctx; }

    // ==================== 构建 ====================

    private void build() {
        wm = (WindowManager) ctx.getSystemService(Context.WINDOW_SERVICE);
        lp = new WindowManager.LayoutParams(
                dp(56), dp(56),
                WindowManager.LayoutParams.TYPE_APPLICATION_OVERLAY,
                WindowManager.LayoutParams.FLAG_NOT_FOCUSABLE
                        | WindowManager.LayoutParams.FLAG_NOT_TOUCH_MODAL,
                PixelFormat.TRANSLUCENT);
        lp.gravity = Gravity.TOP | Gravity.START;
        lp.x = wm.getCurrentWindowMetrics().getBounds().right - dp(56) - dp(16);
        lp.y = dp(240);

        root = buildBall();
        try { wm.addView(root, lp); } catch (Exception e) {
            // 权限不足时记录
            lastInfo = "addView fail: " + e;
        }
    }

    private TextView buildBall() {
        TextView b = new TextView(ctx);
        b.setText("devctl");
        b.setTextColor(Color.WHITE);
        b.setTextSize(11);
        b.setTypeface(Typeface.DEFAULT_BOLD);
        b.setGravity(Gravity.CENTER);
        android.graphics.drawable.GradientDrawable bg = new android.graphics.drawable.GradientDrawable();
        bg.setShape(android.graphics.drawable.GradientDrawable.OVAL);
        bg.setColor(0xE6226DF3);
        b.setBackground(bg);
        b.setElevation(dp(6));
        b.setOnTouchListener(dragOrTap);
        return b;
    }

    // ==================== 面板 ====================

    public void openPanel() {
        if (root == null) return;
        try { wm.removeView(root); } catch (Exception e) {}
        root = null;
        panel = buildPanel();
        lp.width = dp(300);
        lp.height = WindowManager.LayoutParams.WRAP_CONTENT;
        try {
            wm.addView(panel, lp);
        } catch (Exception e) {
            lastInfo = "panel fail: " + e;
        }
        showingPanel = true;
        refresh();
    }

    public void closePanel() {
        if (panel != null) {
            try { wm.removeView(panel); } catch (Exception e) {}
            panel = null;
        }
        root = buildBall();
        lp.width = dp(56);
        lp.height = dp(56);
        try { wm.addView(root, lp); } catch (Exception e) {}
        showingPanel = false;
    }

    private LinearLayout buildPanel() {
        LinearLayout p = new LinearLayout(ctx);
        p.setOrientation(LinearLayout.VERTICAL);
        android.graphics.drawable.GradientDrawable card = new android.graphics.drawable.GradientDrawable();
        card.setCornerRadius(dp(16));
        card.setColor(0xF5FFFFFF);
        p.setBackground(card);
        p.setElevation(dp(10));

        // 标题栏 (拖动把手)
        LinearLayout hdr = new LinearLayout(ctx);
        hdr.setOrientation(LinearLayout.HORIZONTAL);
        hdr.setGravity(Gravity.CENTER_VERTICAL);
        hdr.setPadding(dp(16), dp(10), dp(10), dp(10));
        TextView title = new TextView(ctx);
        title.setText("devctl 面板");
        title.setTextColor(0xFF202124);
        title.setTextSize(15);
        title.setTypeface(Typeface.DEFAULT_BOLD);
        hdr.addView(title, new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1f));
        TextView close = new TextView(ctx);
        close.setText("收起");
        close.setTextColor(0xFF80868B);
        close.setTextSize(12);
        close.setPadding(dp(8), dp(2), dp(8), dp(2));
        close.setOnClickListener(new View.OnClickListener() {
            @Override public void onClick(View v) { closePanel(); }
        });
        hdr.addView(close);
        hdr.setOnTouchListener(dragOrTap);
        p.addView(hdr, new LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT));

        // 信息区 (可滚动)
        infoView = new TextView(ctx);
        infoView.setTextColor(0xFF44474A);
        infoView.setTextSize(13);
        infoView.setPadding(dp(16), dp(4), dp(16), dp(12));
        infoView.setText("正在连接 agent…");
        ScrollView sv = new ScrollView(ctx);
        sv.addView(infoView, new LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT));
        p.addView(sv, new LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, 0, 1f));

        // 按钮行
        LinearLayout row = new LinearLayout(ctx);
        row.setOrientation(LinearLayout.HORIZONTAL);
        row.setPadding(dp(10), 0, dp(10), dp(12));
        row.addView(btn("刷新", new View.OnClickListener() {
            @Override public void onClick(View v) { refresh(); }
        }), btnLp());
        row.addView(btn("收起", new View.OnClickListener() {
            @Override public void onClick(View v) { closePanel(); }
        }), btnLp());
        Button exitB = btn("退出", new View.OnClickListener() {
            @Override public void onClick(View v) { destroy(); }
        });
        row.addView(exitB, btnLp());
        p.addView(row, new LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT));
        return p;
    }

    private Button btn(String text, View.OnClickListener l) {
        Button b = btn(text);
        b.setOnClickListener(l);
        return b;
    }

    private Button btn(String text) {
        Button b = new Button(ctx);
        b.setText(text);
        b.setTextSize(13);
        b.setAllCaps(false);
        return b;
    }

    private LinearLayout.LayoutParams btnLp() {
        LinearLayout.LayoutParams p = new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1f);
        p.setMargins(dp(6), 0, dp(6), 0);
        return p;
    }

    // ==================== 触摸 (球/面板标题共用) ====================

    private View.OnTouchListener dragOrTap = new View.OnTouchListener() {
        @Override
        public boolean onTouch(View v, MotionEvent e) {
            switch (e.getActionMasked()) {
                case MotionEvent.ACTION_DOWN:
                    downRawX = (int) e.getRawX();
                    downRawY = (int) e.getRawY();
                    downLpX = lp.x;
                    downLpY = lp.y;
                    dragging = false;
                    return true;
                case MotionEvent.ACTION_MOVE: {
                    int dx = (int) e.getRawX() - downRawX;
                    int dy = (int) e.getRawY() - downRawY;
                    if (!dragging && (Math.abs(dx) > dp(8) || Math.abs(dy) > dp(8))) dragging = true;
                    if (dragging) {
                        lp.x = downLpX + dx;
                        lp.y = downLpY + dy;
                        try { wm.updateViewLayout(root != null ? root : panel, lp); } catch (Exception ex) {}
                    }
                    return true;
                }
                case MotionEvent.ACTION_UP:
                    if (!dragging) {
                        if (v == root) openPanel();
                    }
                    dragging = false;
                    return true;
            }
            return false;
        }
    };

    // ==================== agent 信息 ====================

    public void refresh() {
        final String[] res = {null};
        Thread t = new Thread(new Runnable() {
            @Override public void run() {
                res[0] = fetchAgentInfo();
                main.post(new Runnable() {
                    @Override public void run() {
                        if (infoView != null) infoView.setText(res[0]);
                    }
                });
            }
        });
        t.setDaemon(true);
        t.start();
    }

    private String fetchAgentInfo() {
        StringBuilder sb = new StringBuilder();
        try {
            SSLContext c = SSLContext.getInstance("TLS");
            c.init(null, new TrustManager[]{new X509TrustManager() {
                public void checkClientTrusted(X509Certificate[] c, String a) {}
                public void checkServerTrusted(X509Certificate[] c, String a) {}
                public X509Certificate[] getAcceptedIssuers() { return new X509Certificate[0]; }
            }}, new SecureRandom());
            SSLSocket s = (SSLSocket) c.getSocketFactory().createSocket();
            s.connect(new InetSocketAddress("127.0.0.1", 5556), 3000);
            s.setSoTimeout(4000);
            Writer w = new OutputStreamWriter(s.getOutputStream(), "UTF-8");
            BufferedReader rd = new BufferedReader(new InputStreamReader(s.getInputStream(), "UTF-8"));
            writeLine(w, "{\"t\":\"hello\",\"token\":\"devctl\"}");
            String ack = rd.readLine();
            JSONObject ak = new JSONObject(ack == null ? "{}" : ack);
            if (!ak.optBoolean("ok", false)) {
                sb.append("鉴权失败: ").append(ak.optString("stderr", "?"));
            } else {
                sb.append("device: ").append(ak.optString("device", "?")).append("\n");
                sb.append("agent: ").append(ak.optString("version", "?")).append("\n");
            }
            writeLine(w, "{\"t\":\"req\",\"id\":1,\"method\":\"sysinfo\"}");
            String line;
            while ((line = rd.readLine()) != null) {
                JSONObject r = new JSONObject(line);
                if (r.optInt("id", -1) == 1) {
                    if (r.optBoolean("ok", false)) {
                        try {
                            JSONObject d = new JSONObject(r.optString("data", "{}"));
                            sb.append("型号: ").append(d.optString("model", "?"))
                              .append(" · ").append(d.optString("brand", "?")).append("\n");
                            sb.append("Android ").append(d.optString("android", "?"))
                              .append(" (SDK ").append(d.optString("sdk", "?")).append(")\n");
                            sb.append("agent ").append(d.optString("version", "?"));
                        } catch (Exception e) { sb.append("parse err"); }
                    }
                    break;
                }
            }
            s.close();
        } catch (Exception e) {
            sb.append("连接失败: ").append(e.getClass().getSimpleName());
        }
        return sb.toString();
    }

    private void writeLine(Writer w, String json) throws Exception {
        w.write(json + "\n");
        w.flush();
    }

    // ==================== 工具 ====================

    private int dp(int v) {
        return Math.round(v * ctx.getResources().getDisplayMetrics().density);
    }

    private void teardown() {
        try {
            if (root != null) wm.removeView(root);
            if (panel != null) wm.removeView(panel);
        } catch (Exception e) {}
        root = null;
        panel = null;
    }
}
