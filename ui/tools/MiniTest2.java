import android.graphics.Bitmap;
import android.graphics.Canvas;
import android.graphics.Rect;
import android.graphics.RectF;
import java.io.FileDescriptor;
import java.io.FileOutputStream;
import java.io.PrintStream;
import java.lang.reflect.Constructor;
import java.lang.reflect.Method;

public class MiniTest2 {
    public static void main(String[] args) throws Exception {
        System.setOut(new PrintStream(new FileOutputStream(FileDescriptor.out), true));
        System.out.println("[m2] start");

        Class<?> scClass = Class.forName("android.view.SurfaceControl");
        Class<?> builderCls = Class.forName("android.view.SurfaceControl$Builder");
        Class<?> transCls = Class.forName("android.view.SurfaceControl$Transaction");
        Class<?> surfCls = Class.forName("android.view.Surface");
        Class<?> canvasCls = Class.forName("android.graphics.Canvas");
        Class<?> rectCls = Class.forName("android.graphics.Rect");

        Constructor<?> bu = builderCls.getDeclaredConstructor();
        bu.setAccessible(true);
        Object b = bu.newInstance();
        builderCls.getMethod("setName", String.class).invoke(b, "MT2");
        builderCls.getMethod("setBufferSize", int.class, int.class).invoke(b, 150, 150);
        Method mFmt = builderCls.getMethod("setFormat", int.class); mFmt.setAccessible(true);
        mFmt.invoke(b, 1);
        Object sc = builderCls.getMethod("build").invoke(b);
        System.out.println("[m2] sc built");

        Constructor<?> tc = transCls.getDeclaredConstructor();
        tc.setAccessible(true);
        Object trans = tc.newInstance();
        transCls.getMethod("setLayer", scClass, int.class).invoke(trans, sc, Integer.MAX_VALUE - 100);
        transCls.getMethod("setPosition", scClass, float.class, float.class).invoke(trans, sc, 50f, 800f);
        transCls.getMethod("show", scClass).invoke(trans, sc);
        transCls.getMethod("apply").invoke(trans);
        System.out.println("[m2] shown");

        Constructor<?> sctor = surfCls.getDeclaredConstructor(scClass);
        sctor.setAccessible(true);
        Object surface = sctor.newInstance(sc);
        System.out.println("[m2] surface obj");

        Method mLock = surfCls.getMethod("lockCanvas", rectCls);
        Method mUnlock = surfCls.getMethod("unlockCanvasAndPost", canvasCls);

        Object r = rectCls.getDeclaredConstructor(int.class, int.class, int.class, int.class).newInstance(0, 0, 150, 150);
        Canvas c = (Canvas) mLock.invoke(surface, r);
        System.out.println("[m2] locked canvas=" + c);
        canvasCls.getMethod("drawColor", int.class).invoke(c, 0xE6226DF3);
        System.out.println("[m2] drawColor ok");

        // drawBitmap (null paint)
        try {
            Bitmap bm = Bitmap.createBitmap(64, 64, Bitmap.Config.ARGB_8888);
            int[] px = new int[64 * 64];
            for (int i = 0; i < px.length; i++) px[i] = 0xFFFFFFFF;
            bm.setPixels(px, 0, 64, 0, 0, 64, 64);
            Method mDb = canvasCls.getMethod("drawBitmap", Bitmap.class, float.class, float.class, android.graphics.Paint.class);
            mDb.invoke(c, bm, 40f, 40f, null);
            System.out.println("[m2] drawBitmap(float) ok");
        } catch (Throwable t) {
            System.out.println("[m2] drawBitmap(float) FAIL: " + t);
        }

        // drawBitmap(src,dst,null)
        try {
            Bitmap bm = Bitmap.createBitmap(64, 64, Bitmap.Config.ARGB_8888);
            int[] px = new int[64 * 64];
            for (int i = 0; i < px.length; i++) px[i] = 0xFFFFFFFF;
            bm.setPixels(px, 0, 64, 0, 0, 64, 64);
            Method mDb = canvasCls.getMethod("drawBitmap", Bitmap.class, Rect.class, RectF.class, android.graphics.Paint.class);
            Rect src = new Rect(0, 0, 64, 64);
            RectF dst = new RectF(10, 10, 80, 80);
            mDb.invoke(c, bm, src, dst, null);
            System.out.println("[m2] drawBitmap(rect) ok");
        } catch (Throwable t) {
            System.out.println("[m2] drawBitmap(rect) FAIL: " + t);
        }

        mUnlock.invoke(surface, c);
        System.out.println("[m2] posted");

        // 保持显示 8 秒, 期间不崩溃即通过
        Thread.sleep(8000);
        System.out.println("[m2] done (8s alive)");
    }
}
