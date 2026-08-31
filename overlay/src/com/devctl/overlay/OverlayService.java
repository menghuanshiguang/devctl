package com.devctl.overlay;

import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.app.Service;
import android.content.Intent;
import android.content.pm.ServiceInfo;
import android.graphics.Color;
import android.graphics.PixelFormat;
import android.graphics.Point;
import android.graphics.Rect;
import android.graphics.Typeface;
import android.graphics.drawable.GradientDrawable;
import android.os.Build;
import android.os.Handler;
import android.os.IBinder;
import android.os.Looper;
import android.view.Gravity;
import android.view.MotionEvent;
import android.view.View;
import android.view.ViewGroup;
import android.view.WindowManager;
import android.widget.FrameLayout;
import android.widget.LinearLayout;
import android.widget.TextView;

import org.json.JSONObject;

import java.io.BufferedReader;
import java.io.InputStreamReader;
import java.io.OutputStream;
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
 * devctl 悬浮控件 (悬浮球 + 二级面板)
 *
 * 悬浮球: 可拖动, 轻点展开二级面板
 * 面板:   显示 agent 设备信息 (TLS 连接本机 5556 端口),
 *         可拖动 (标题栏), 按钮: 刷新 / 收起 / 退出
 *
 * 启动 (root): am start-foreground-service -n com.devctl.overlay/.OverlayService --es token devctl
 */
public class OverlayService extends Service {

    private static final String PKG = "com.devctl.overlay";
    private static final int PORT = 5556;
    private static final int BALL_SIZE_DP = 52;
    private static final int PANEL_W_DP = 300;

    private WindowManager wm;
    private FrameLayout root;              // 唯一的悬浮窗口容器 (球/面板二选一)
    private TextView ball;
    private LinearLayout panel;
    private WindowManager.LayoutParams lp;
    private TextView infoView;

    private String token = "devctl";
    private boolean created = false;
    private boolean showingPanel = false;

    // 拖动状态
    private int touchRawX, touchRawY;      // 手指按下时 raw 坐标
    private int downLpX, downLpY;          // 按下时窗口位置
    private boolean dragging = false;

    private final Handler h = new Handler(Looper.getMainLooper());

    @Override
    public IBinder onBind(Intent intent) { return null; }

    @Override
    public void onCreate() {
        super.onCreate();
        wm = (WindowManager) getSystemService(WINDOW_SERVICE);

        NotificationManager nm = (NotificationManager) getSystemService(NOTIFICATION_SERVICE);
        NotificationChannel ch = new NotificationChannel("ov", "devctl overlay", NotificationManager.IMPORTANCE_LOW);
        nm.createNotificationChannel(ch);
        Notification n = new Notification.Builder(this, "ov")
                .setContentTitle("devctl overlay")
                .setContentText("悬浮控件运行中")
                .setSmallIcon(android.R.drawable.ic_menu_manage)
                .build();
        if (Build.VERSION.SDK_INT >= 29) {
            startForeground(1, n, ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE);
        } else {
            startForeground(1, n);
        }
    }

    @Override
    public int onStartCommand(Intent intent, int flags, int startId) {
        if (intent != null) {
            String t = intent.getStringExtra("token");
            if (t != null && t.length() > 0) token = t;
        }
        if (!created) {
            created = true;
            createBall();
        }
        return START_STICKY;
    }

    @Override
    public void onDestroy() {
        try {
            if (root != null && root.isAttachedToWindow()) wm.removeView(root);
        } catch (Exception ignored) {}
        root = null;
        super.onDestroy();
    }

    // ---------- 悬浮球 ----------

    private void createBall() {
        int size = dp(BALL_SIZE_DP);
        ball = new TextView(this);
        ball.setText("D");
        ball.setTextColor(Color.WHITE);
        ball.setTextSize(20);
        ball.setGravity(Gravity.CENTER);
        ball.setTypeface(Typeface.DEFAULT_BOLD);

        GradientDrawable bg = new GradientDrawable();
        bg.setShape(GradientDrawable.OVAL);
        bg.setColor(0xE6226DF3);
        ball.setBackground(bg);
        ball.setElevation(dp(6));
        ball.setOnTouchListener(dragOrTap);

        lp = new WindowManager.LayoutParams(
                size, size,
                WindowManager.LayoutParams.TYPE_APPLICATION_OVERLAY,
                WindowManager.LayoutParams.FLAG_NOT_FOCUSABLE
                        | WindowManager.LayoutParams.FLAG_NOT_TOUCH_MODAL,
                PixelFormat.TRANSLUCENT);
        lp.gravity = Gravity.TOP | Gravity.START;
        Rect scr = screenRect();
        lp.x = scr.right - size - dp(16);
        lp.y = dp(240);

        root = new FrameLayout(this);
        root.addView(ball);
        if (!root.isAttachedToWindow()) wm.addView(root, lp);
    }

    // ---------- 二级面板 ----------

    private void showPanel() {
        showingPanel = true;
        root.removeAllViews();
        panel = buildPanel();
        root.addView(panel);
        lp.width = dp(PANEL_W_DP);
        lp.height = ViewGroup.LayoutParams.WRAP_CONTENT;
        clampWindow();
        wm.updateViewLayout(root, lp);
        fetchInfo();
    }

    private void hidePanel() {
        showingPanel = false;
        root.removeAllViews();
        root.addView(ball);
        lp.width = dp(BALL_SIZE_DP);
        lp.height = dp(BALL_SIZE_DP);
        clampWindow();
        wm.updateViewLayout(root, lp);
    }

    private void quit() {
        stopSelf();
    }

    private LinearLayout buildPanel() {
        LinearLayout p = new LinearLayout(this);
        p.setOrientation(LinearLayout.VERTICAL);

        GradientDrawable card = new GradientDrawable();
        card.setColor(0xF5FFFFFF);
        card.setCornerRadius(dp(16));
        p.setBackground(card);
        p.setElevation(dp(10));

        // ---- 标题栏 (拖动把手: 拖动移动窗口, 轻点收起) ----
        LinearLayout hdr = new LinearLayout(this);
        hdr.setOrientation(LinearLayout.HORIZONTAL);
        hdr.setGravity(Gravity.CENTER_VERTICAL);
        hdr.setPadding(dp(16), dp(10), dp(10), dp(10));
        TextView title = new TextView(this);
        title.setText("devctl 面板");
        title.setTextColor(0xFF202124);
        title.setTextSize(15);
        title.setTypeface(Typeface.DEFAULT_BOLD);
        hdr.addView(title, new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1f));
        TextView close = new TextView(this);
        close.setText("收起");
        close.setTextColor(0xFF80868B);
        close.setTextSize(12);
        close.setPadding(dp(8), dp(2), dp(8), dp(2));
        close.setOnClickListener(new View.OnClickListener() {
            @Override public void onClick(View v) { hidePanel(); }
        });
        hdr.addView(close);
        hdr.setOnTouchListener(dragOrTap);
        p.addView(hdr, new LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT));

        // ---- 信息区 ----
        infoView = new TextView(this);
        infoView.setTextColor(0xFF44474A);
        infoView.setTextSize(13);
        infoView.setLineSpacing(4, 1.15f);
        infoView.setPadding(dp(16), dp(2), dp(16), dp(12));
        infoView.setText("正在连接 agent…");
        p.addView(infoView, new LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT));

        // ---- 按钮行 ----
        LinearLayout row = new LinearLayout(this);
        row.setOrientation(LinearLayout.HORIZONTAL);
        row.setPadding(dp(10), 0, dp(10), dp(12));

        TextView refresh = smallBtn("刷新");
        refresh.setOnClickListener(new View.OnClickListener() {
            @Override public void onClick(View v) { fetchInfo(); }
        });
        TextView collapse = smallBtn("收起");
        collapse.setOnClickListener(new View.OnClickListener() {
            @Override public void onClick(View v) { hidePanel(); }
        });
        TextView exit = smallBtn("退出");
        exit.setOnClickListener(new View.OnClickListener() {
            @Override public void onClick(View v) { quit(); }
        });
        row.addView(refresh, btnParams());
        row.addView(collapse, btnParams());
        row.addView(exit, btnParams());
        p.addView(row, new LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT));

        return p;
    }

    private TextView smallBtn(String text) {
        TextView t = new TextView(this);
        t.setText(text);
        t.setTextColor(0xFF2266EE);
        t.setTextSize(13);
        t.setGravity(Gravity.CENTER);
        t.setPadding(0, dp(8), 0, dp(8));
        GradientDrawable g = new GradientDrawable();
        g.setCornerRadius(dp(10));
        g.setColor(0xFFEFF4FF);
        t.setBackground(g);
        return t;
    }

    private LinearLayout.LayoutParams btnParams() {
        LinearLayout.LayoutParams p = new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1f);
        p.setMargins(dp(6), 0, dp(6), 0);
        return p;
    }

    // ---------- 拖动 / 点击 (球与面板标题栏共用) ----------

    private final View.OnTouchListener dragOrTap = new View.OnTouchListener() {
        @Override
        public boolean onTouch(View v, MotionEvent e) {
            switch (e.getActionMasked()) {
                case MotionEvent.ACTION_DOWN:
                    touchRawX = (int) e.getRawX();
                    touchRawY = (int) e.getRawY();
                    downLpX = lp.x;
                    downLpY = lp.y;
                    dragging = false;
                    if (root != null && root.isAttachedToWindow()) {
                        try { wm.updateViewLayout(root, lp); } catch (Exception ignored) {}
                    }
                    return true;
                case MotionEvent.ACTION_MOVE: {
                    int dx = (int) e.getRawX() - touchRawX;
                    int dy = (int) e.getRawY() - touchRawY;
                    if (!dragging && (Math.abs(dx) > dp(8) || Math.abs(dy) > dp(8))) dragging = true;
                    if (dragging) {
                        lp.x = downLpX + dx;
                        lp.y = downLpY + dy;
                        clampWindow();
                        wm.updateViewLayout(root, lp);
                    }
                    return true;
                }
                case MotionEvent.ACTION_UP:
                    if (!dragging) {
                        if (v == ball) showPanel();
                        else if (showingPanel) hidePanel();
                    }
                    dragging = false;
                    return true;
            }
            return false;
        }
    };

    // ---------- 窗口位置钳制 ----------

    private void clampWindow() {
        Rect scr = screenRect();
        int w = lp.width, h = lp.height;
        int maxX = Math.max(0, scr.right - w - dp(4));
        int maxY = Math.max(0, scr.bottom - h - dp(4));
        if (lp.x < dp(4)) lp.x = dp(4);
        if (lp.y < dp(30)) lp.y = dp(30);
        if (lp.x > maxX) lp.x = maxX;
        if (lp.y > maxY && lp.height != ViewGroup.LayoutParams.WRAP_CONTENT) lp.y = maxY;
    }

    private Rect screenRect() {
        if (Build.VERSION.SDK_INT >= 30) {
            return wm.getCurrentWindowMetrics().getBounds();
        }
        Point p = new Point();
        wm.getDefaultDisplay().getRealSize(p);
        return new Rect(0, 0, p.x, p.y);
    }

    private int dp(int v) {
        return Math.round(v * getResources().getDisplayMetrics().density);
    }

    // ---------- agent 通信 (TLS → 127.0.0.1:5556) ----------

    private void fetchInfo() {
        h.post(new Runnable() {
            @Override public void run() {
                if (infoView != null) infoView.setText("正在连接 agent…");
            }
        });
        Thread t = new Thread(new Runnable() {
            @Override public void run() { doFetch(); }
        });
        t.setDaemon(true);
        t.start();
    }

    private void doFetch() {
        final StringBuilder sb = new StringBuilder();
        try {
            SSLContext ctx = SSLContext.getInstance("TLS");
            ctx.init(null, new TrustManager[]{new X509TrustManager() {
                @Override public void checkClientTrusted(X509Certificate[] chain, String authType) {}
                @Override public void checkServerTrusted(X509Certificate[] chain, String authType) {}
                @Override public X509Certificate[] getAcceptedIssuers() { return new X509Certificate[0]; }
            }}, new SecureRandom());

            SSLSocket s = (SSLSocket) ctx.getSocketFactory().createSocket();
            s.connect(new InetSocketAddress("127.0.0.1", PORT), 4000);
            s.setSoTimeout(5000);

            Writer w = new OutputStreamWriter(s.getOutputStream(), "UTF-8");
            BufferedReader rd = new BufferedReader(new InputStreamReader(s.getInputStream(), "UTF-8"), 8192);

            // hello
            JSONObject hello = new JSONObject();
            hello.put("t", "hello");
            hello.put("token", token);
            w.write(hello.toString() + "\n");
            w.flush();

            String ack = rd.readLine();
            JSONObject ackObj = new JSONObject(ack == null ? "{}" : ack);
            boolean ackOk = ackObj.optBoolean("ok", false);
            if (!ackOk) {
                sb.append("agent 鉴权失败 (token?)");
            } else {
                sb.append("device: ").append(ackObj.optString("device", "?")).append("\n");
                sb.append("agent: ").append(ackObj.optString("version", "?")).append("\n");
            }

            // sysinfo
            JSONObject req = new JSONObject();
            req.put("t", "req");
            req.put("id", 1);
            req.put("method", "sysinfo");
            w.write(req.toString() + "\n");
            w.flush();

            String line;
            while ((line = rd.readLine()) != null) {
                JSONObject r = new JSONObject(line);
                if (r.optInt("id", -1) == 1 && "res".equals(r.optString("t"))) {
                    if (r.optBoolean("ok", false)) {
                        JSONObject d = new JSONObject(r.optString("data", "{}"));
                        sb.append("型号: ").append(d.optString("model", "?")).append(" · ")
                          .append(d.optString("brand", "?")).append("\n");
                        sb.append("Android ").append(d.optString("android", "?"))
                          .append(" (SDK ").append(d.optString("sdk", "?")).append(")\n");
                        sb.append("架构: ").append(d.optString("goarch", "?"));
                    } else {
                        sb.append("sysinfo 失败: ").append(r.optString("stderr", ""));
                    }
                    break;
                }
            }
            s.close();
        } catch (Exception e) {
            sb.setLength(0);
            sb.append("连接失败: ").append(e.getClass().getSimpleName())
              .append(" ").append(e.getMessage() == null ? "" : e.getMessage());
        }
        final String txt = sb.toString();
        h.post(new Runnable() {
            @Override public void run() {
                if (infoView != null) infoView.setText(txt);
            }
        });
    }
}
