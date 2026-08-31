import android.graphics.Bitmap;
import android.graphics.Canvas;
import android.graphics.Color;
import android.graphics.Paint;
import android.graphics.Typeface;
import java.io.FileOutputStream;

public class Typetest2 {
    static void log(String s) { System.out.println("[t2] " + s); }

    public static void main(String[] args) throws Exception {
        log("start");
        // 1) 默认 Paint measureText (不碰 Typeface.create)
        try {
            Paint p = new Paint();
            p.setTextSize(48);
            log("paint measure default ok w=" + p.measureText("devctl"));
        } catch (Throwable t) { log("FAIL paint measure: " + t); }

        // 2) Typeface.DEFAULT 静态引用
        try {
            Paint p = new Paint();
            p.setTypeface(Typeface.DEFAULT);
            p.setTextSize(48);
            log("DEFAULT measure ok w=" + p.measureText("devctl"));
        } catch (Throwable t) { log("FAIL DEFAULT: " + t); }

        // 3) Bitmap 上画 ASCII
        try {
            Bitmap bm = Bitmap.createBitmap(560, 200, Bitmap.Config.ARGB_8888);
            Canvas c = new Canvas(bm);
            c.drawColor(Color.RED);
            Paint p = new Paint();
            p.setTypeface(Typeface.DEFAULT);
            p.setColor(Color.WHITE);
            p.setTextSize(44);
            c.drawText("devctl PKX110 SDK36", 20, 70, p);
            FileOutputStream fo = new FileOutputStream("/data/local/tmp/tt.png");
            bm.compress(Bitmap.CompressFormat.PNG, 100, fo);
            fo.close();
            log("ascii bitmap saved");
        } catch (Throwable t) { log("FAIL ascii bmp: " + t); }

        // 4) Bitmap 上画中文
        try {
            Bitmap bm = Bitmap.createBitmap(560, 200, Bitmap.Config.ARGB_8888);
            Canvas c = new Canvas(bm);
            c.drawColor(Color.BLUE);
            Paint p = new Paint();
            p.setTypeface(Typeface.DEFAULT);
            p.setColor(Color.WHITE);
            p.setTextSize(44);
            c.drawText("面板 刷新 收起 退出", 20, 70, p);
            FileOutputStream fo = new FileOutputStream("/data/local/tmp/tt2.png");
            bm.compress(Bitmap.CompressFormat.PNG, 100, fo);
            fo.close();
            log("chinese bitmap saved");
        } catch (Throwable t) { log("FAIL chinese bmp: " + t); }

        log("done");
    }
}
