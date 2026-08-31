import android.view.View;
import android.widget.TextView;
import android.content.Context;
import android.content.res.Resources;
import android.graphics.Canvas;
import android.util.TypedValue;
import java.lang.reflect.Method;

/**
 * NvTest — PoC: 在 app_process 里用原生 TextView 离线渲染 (View.draw(Canvas))
 * 验证: ActivityThread.systemMain() 拿 Context 后, Typeface/文字能否工作
 * 用法: app_process -Djava.class.path=<dex> /system/bin NvTest
 */
public class NvTest {
    public static void main(String[] args) throws Exception {
        System.out.println("[nvtest] starting");
        System.out.flush();

        // 1) 反射 ActivityThread.systemMain() 拿系统 Context
        Class<?> atClass = Class.forName("android.app.ActivityThread");
        Method sysMain = atClass.getDeclaredMethod("systemMain");
        sysMain.setAccessible(true);
        Object at = sysMain.invoke(null);
        Method getCtx = atClass.getDeclaredMethod("getSystemContext");
        getCtx.setAccessible(true);
        Context ctx = (Context) getCtx.invoke(at);
        System.out.println("[nvtest] context=" + ctx);
        System.out.flush();

        // 2) new TextView (原生控件!)
        TextView tv = new TextView(ctx);
        tv.setText("NvTest 原生控件文字 123 ABC");
        tv.setTextSize(TypedValue.COMPLEX_UNIT_SP, 24);
        tv.setTextColor(0xFFFFFFFF);

        // 3) 手动 measure + layout
        int wSpec = View.MeasureSpec.makeMeasureSpec(900, View.MeasureSpec.EXACTLY);
        int hSpec = View.MeasureSpec.makeMeasureSpec(120, View.MeasureSpec.EXACTLY);
        tv.measure(wSpec, hSpec);
        tv.layout(0, 0, 900, 120);
        System.out.println("[nvtest] measured " + tv.getMeasuredWidth() + "x" + tv.getMeasuredHeight());
        System.out.flush();

        // 4) 创建 Bitmap Canvas 离线画 (先验证不崩)
        android.graphics.Bitmap bmp = android.graphics.Bitmap.createBitmap(900, 120, android.graphics.Bitmap.Config.ARGB_8888);
        Canvas c = new Canvas(bmp);
        c.drawColor(0xFF222222);
        tv.draw(c);   // 原生 View.draw(Canvas)
        System.out.println("[nvtest] TV drawn to bitmap OK");
        System.out.flush();

        // 保存验证
        java.io.FileOutputStream fo = new java.io.FileOutputStream("/data/local/devctl/nvtest.png");
        bmp.compress(android.graphics.Bitmap.CompressFormat.PNG, 100, fo);
        fo.close();
        System.out.println("[nvtest] saved /data/local/devctl/nvtest.png");
        System.out.flush();
    }
}
