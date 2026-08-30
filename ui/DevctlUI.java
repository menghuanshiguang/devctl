import java.lang.reflect.Constructor;
import java.lang.reflect.Method;

public class DevctlUI {
    static Object sc;
    static Object surface;
    static Object trans;

    public static void main(String[] args) throws Exception {
        // 不传参或传 0 = 无限持续显示
        int seconds = args.length > 0 ? Integer.parseInt(args[0]) : 0;

        Class<?> scClass = Class.forName("android.view.SurfaceControl");
        Class<?> builderCls = Class.forName("android.view.SurfaceControl$Builder");
        Class<?> transCls = Class.forName("android.view.SurfaceControl$Transaction");
        Class<?> surfCls = Class.forName("android.view.Surface");
        Class<?> canvasCls = Class.forName("android.graphics.Canvas");
        Class<?> rectCls = Class.forName("android.graphics.Rect");

        Constructor<?> bu = builderCls.getDeclaredConstructor();
        bu.setAccessible(true);
        Object builder = bu.newInstance();
        builderCls.getMethod("setName", String.class).invoke(builder, "DevctlUI");
        builderCls.getMethod("setBufferSize", int.class, int.class).invoke(builder, 840, 180);
        Method mFormat = builderCls.getMethod("setFormat", int.class);
        mFormat.setAccessible(true);
        mFormat.invoke(builder, 1);
        sc = builderCls.getMethod("build").invoke(builder);
        System.out.println("[DevctlUI] surface created ok");

        Constructor<?> tc = transCls.getDeclaredConstructor();
        tc.setAccessible(true);
        trans = tc.newInstance();
        Method mSetLayer = transCls.getMethod("setLayer", scClass, int.class);
        Method mSetPos = transCls.getMethod("setPosition", scClass, float.class, float.class);
        Method mApply = transCls.getMethod("apply");
        mSetLayer.setAccessible(true);
        mSetPos.setAccessible(true);
        mSetLayer.invoke(trans, sc, Integer.MAX_VALUE - 100);
        mSetPos.invoke(trans, sc, 24f, 240f);
        // 此 ROM (ColorOS) 无 setVisible, 用 show(SurfaceControl)
        transCls.getMethod("show", scClass).invoke(trans, sc);
        mApply.invoke(trans);
        System.out.println("[DevctlUI] shown ok");

        Constructor<?> sctor = surfCls.getDeclaredConstructor(scClass);
        sctor.setAccessible(true);
        surface = sctor.newInstance(sc);
        Method mLock = surfCls.getMethod("lockCanvas", rectCls);
        Method mUnlockPost = surfCls.getMethod("unlockCanvasAndPost", canvasCls);

        long t0 = System.currentTimeMillis();
        int[] colors = {0xE6101418, 0xE6104818, 0xE6101438, 0xE6441418};
        int frame = 0;
        while (seconds <= 0 || System.currentTimeMillis() - t0 < seconds * 1000L) {
            Object rect = rectCls.getDeclaredConstructor(int.class, int.class, int.class, int.class)
                    .newInstance(0, 0, 840, 180);
            Object canvas = mLock.invoke(surface, rect);
            if (canvas == null) {
                Thread.sleep(300);
                continue;
            }
            canvasCls.getMethod("drawColor", int.class).invoke(canvas, colors[frame % colors.length]);
            mUnlockPost.invoke(surface, canvas);
            frame++;
            Thread.sleep(100);
        }

        surfCls.getMethod("release").invoke(surface);
        Method mRemove = transCls.getMethod("remove", scClass);
        mRemove.setAccessible(true);
        mRemove.invoke(trans, sc);
        mApply.invoke(trans);
        System.out.println("[DevctlUI] exited, layer removed");
    }
}
