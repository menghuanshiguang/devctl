import android.graphics.Bitmap;
import android.graphics.Canvas;
import java.io.BufferedReader;
import java.io.DataInputStream;
import java.io.FileDescriptor;
import java.io.FileInputStream;
import java.io.FileOutputStream;
import java.io.InputStreamReader;
import java.io.PrintStream;
import java.lang.reflect.Constructor;
import java.lang.reflect.Method;
import java.util.ArrayList;

public class OverlayMin {
    static void log(String s) { System.out.println("[om] " + s); System.out.flush(); }

    public static void main(String[] args) throws Exception {
        System.setOut(new PrintStream(new FileOutputStream(FileDescriptor.out), true));
        log("start");
        initSurface();
        log("surface done");
        loadFont();
        log("font done");
        reloadInfo();
        log("info done: " + infoStr());
        drawBallTest();
        log("drawBall done");
        log("ALL OK");
    }

    static String infoStr() { return info1 + " | " + info2 + " | " + info3; }
    static String info1 = "", info2 = "", info3 = "";

    // ---- 反射句柄 (与 DevctlOverlay 相同) ----
    static Object scBall, transObj, surfaceBall;
    static Class<?> canvasCls, rectCls;
    static Method mLock, mUnlock, mApply, mSetPos, mDrawColor;

    static void initSurface() throws Exception {
        Class<?> scClass = Class.forName("android.view.SurfaceControl");
        Class<?> builderCls = Class.forName("android.view.SurfaceControl$Builder");
        Class<?> transCls = Class.forName("android.view.SurfaceControl$Transaction");
        Class<?> surfCls = Class.forName("android.view.Surface");
        canvasCls = Class.forName("android.graphics.Canvas");
        rectCls = Class.forName("android.graphics.Rect");

        Constructor<?> bu = builderCls.getDeclaredConstructor(); bu.setAccessible(true);
        Object b = bu.newInstance();
        builderCls.getMethod("setName", String.class).invoke(b, "OvMinBall");
        builderCls.getMethod("setBufferSize", int.class, int.class).invoke(b, 150, 150);
        Method mFmt = builderCls.getMethod("setFormat", int.class); mFmt.setAccessible(true);
        mFmt.invoke(b, 1);
        scBall = builderCls.getMethod("build").invoke(b);

        Constructor<?> tc = transCls.getDeclaredConstructor(); tc.setAccessible(true);
        transObj = tc.newInstance();
        Method mSetLayer = transCls.getMethod("setLayer", scClass, int.class);
        mSetPos = transCls.getMethod("setPosition", scClass, float.class, float.class);
        mApply = transCls.getMethod("apply");
        Method mShow = transCls.getMethod("show", scClass);
        mSetLayer.setAccessible(true); mSetPos.setAccessible(true); mApply.setAccessible(true); mShow.setAccessible(true);
        mSetLayer.invoke(transObj, scBall, Integer.MAX_VALUE - 100);
        mSetPos.invoke(transObj, scBall, 300f, 900f);
        mShow.invoke(transObj, scBall);
        mApply.invoke(transObj);

        Constructor<?> sctor = surfCls.getDeclaredConstructor(scClass); sctor.setAccessible(true);
        surfaceBall = sctor.newInstance(scBall);
        mLock = surfCls.getMethod("lockCanvas", rectCls);
        mUnlock = surfCls.getMethod("unlockCanvasAndPost", canvasCls);
        mDrawColor = canvasCls.getMethod("drawColor", int.class);
    }

    static Bitmap[] glyphs;
    static int glyphCount = 0;

    static void loadFont() throws Exception {
        DataInputStream in = new DataInputStream(new FileInputStream("/data/local/tmp/devfont.bin"));
        int len = in.readUnsignedShort();
        byte[] buf = new byte[len];
        in.readFully(buf);
        String chars = new String(buf, "UTF-8");
        log("font header ok chars=" + chars.length());
        int cellBytes = 64 * 64 / 8;
        byte[] cell = new byte[cellBytes];
        glyphs = new Bitmap[chars.length()];
        for (int i = 0; i < chars.length(); i++) {
            in.readFully(cell);
            int[] px = new int[64 * 64];
            for (int y = 0; y < 64; y++)
                for (int x = 0; x < 64; x++) {
                    int bit = (cell[y * 8 + x / 8] >> (7 - (x & 7))) & 1;
                    px[y * 64 + x] = bit == 1 ? 0xFFFFFFFF : 0;
                }
            Bitmap bm = Bitmap.createBitmap(64, 64, Bitmap.Config.ARGB_8888);
            bm.setPixels(px, 0, 64, 0, 0, 64, 64);
            glyphs[i] = bm;
            glyphCount++;
            if (i % 60 == 0) log("glyph " + i);
        }
        in.close();
    }

    static void reloadInfo() {
        info1 = prop("ro.product.model");
        info2 = prop("ro.build.version.release");
        info3 = prop("ro.build.version.sdk");
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

    static void drawBallTest() throws Exception {
        Object r = rectCls.getDeclaredConstructor(int.class, int.class, int.class, int.class).newInstance(0, 0, 150, 150);
        Canvas c = (Canvas) mLock.invoke(surfaceBall, r);
        log("locked: " + c);
        mDrawColor.invoke(c, 0xE6226DF3);
        log("drawColor ok");
        // 画第一个 glyph
        if (glyphCount > 0) {
            android.graphics.Rect src = new android.graphics.Rect(0, 0, 64, 64);
            android.graphics.RectF dst = new android.graphics.RectF(40, 40, 104, 104);
            Method mDb = canvasCls.getMethod("drawBitmap", Bitmap.class, android.graphics.Rect.class, android.graphics.RectF.class, android.graphics.Paint.class);
            mDb.invoke(c, glyphs[0], src, dst, null);
            log("drawBitmap ok");
        }
        mUnlock.invoke(surfaceBall, c);
        log("posted");
    }
}
