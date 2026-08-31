import android.graphics.Bitmap;
import java.io.BufferedReader;
import java.io.DataInputStream;
import java.io.FileInputStream;
import java.io.InputStreamReader;
import java.io.PrintStream;
import java.io.FileDescriptor;
import java.io.FileOutputStream;

public class MiniTest {
    static void log(String s) {
        System.out.println("[m] " + s);
        System.out.flush();
    }

    public static void main(String[] args) throws Exception {
        System.setOut(new PrintStream(new FileOutputStream(FileDescriptor.out), true));
        log("start");

        // 1) getprop exec
        try {
            Process p = Runtime.getRuntime().exec(new String[]{"/system/bin/getprop", "ro.product.model"});
            BufferedReader r = new BufferedReader(new InputStreamReader(p.getInputStream()));
            String s = r.readLine();
            p.waitFor();
            log("getprop ok: " + s);
        } catch (Throwable t) { log("getprop FAIL " + t); }

        // 2) /proc/meminfo
        try {
            BufferedReader r = new BufferedReader(new InputStreamReader(new FileInputStream("/proc/meminfo")));
            log("meminfo ok: " + r.readLine());
            r.close();
        } catch (Throwable t) { log("meminfo FAIL " + t); }

        // 3) DevFont 完整加载
        try {
            DataInputStream in = new DataInputStream(new FileInputStream("/data/local/tmp/devfont.bin"));
            int len = in.readUnsignedShort();
            byte[] buf = new byte[len];
            in.readFully(buf);
            String chars = new String(buf, "UTF-8");
            log("font header ok chars=" + chars.length());
            int cellBytes = 64 * 64 / 8;
            byte[] cell = new byte[cellBytes];
            int built = 0;
            for (int i = 0; i < chars.length(); i++) {
                in.readFully(cell);
                int[] px = new int[64 * 64];
                for (int y = 0; y < 64; y++) {
                    for (int x = 0; x < 64; x++) {
                        int bit = (cell[y * 8 + x / 8] >> (7 - (x & 7))) & 1;
                        px[y * 64 + x] = bit == 1 ? 0xFFFFFFFF : 0;
                    }
                }
                Bitmap bm = Bitmap.createBitmap(64, 64, Bitmap.Config.ARGB_8888);
                bm.setPixels(px, 0, 64, 0, 0, 64, 64);
                built++;
                if (built % 50 == 0) log("built " + built);
            }
            in.close();
            log("all glyphs built: " + built);
        } catch (Throwable t) { log("font FAIL " + t); }

        // 4) 屏幕尺寸 sh 命令
        try {
            Process p = Runtime.getRuntime().exec(new String[]{"/system/bin/sh", "-c", "wm size"});
            BufferedReader r = new BufferedReader(new InputStreamReader(p.getInputStream()));
            log("wm size: " + r.readLine());
            p.waitFor();
        } catch (Throwable t) { log("wm FAIL " + t); }

        log("done");
    }
}
