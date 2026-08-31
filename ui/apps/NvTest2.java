import android.widget.TextView;
import android.graphics.Canvas;
import android.graphics.Bitmap;

/** NvTest2 — 不经过 ActivityThread, 直接测 TextView/Bitmap/Typeface 在 app_process 能否工作 */
public class NvTest2 {
    public static void main(String[] args) throws Exception {
        System.out.println("[nvtest2] step1: Bitmap");
        System.out.flush();
        Bitmap bmp = Bitmap.createBitmap(200, 60, Bitmap.Config.ARGB_8888);
        Canvas c = new Canvas(bmp);
        c.drawColor(0xFF333333);
        System.out.println("[nvtest2] step2: Bitmap OK");
        System.out.flush();

        System.out.println("[nvtest2] step3: Typeface");
        System.out.flush();
        android.graphics.Typeface tf = android.graphics.Typeface.create("sans-serif", android.graphics.Typeface.NORMAL);
        System.out.println("[nvtest2] Typeface=" + tf);
        System.out.flush();

        System.out.println("[nvtest2] step4: done");
        System.out.flush();
    }
}
