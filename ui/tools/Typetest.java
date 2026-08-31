import android.graphics.Paint;
import android.graphics.Typeface;

public class Typetest {

    interface F { Object get() throws Exception; }

    public static void main(String[] args) throws Exception {
        System.out.println("[t] start");
        test("create sans-serif NORMAL", new F() { public Object get() throws Exception { return Typeface.create("sans-serif", Typeface.NORMAL); } });
        test("create sans-serif BOLD", new F() { public Object get() throws Exception { return Typeface.create("sans-serif", Typeface.BOLD); } });
        test("create monospace", new F() { public Object get() throws Exception { return Typeface.create("monospace", Typeface.NORMAL); } });
        test("DroidSans.ttf", new F() { public Object get() throws Exception { return Typeface.createFromFile("/system/fonts/DroidSans.ttf"); } });
        test("DroidSans-Bold.ttf", new F() { public Object get() throws Exception { return Typeface.createFromFile("/system/fonts/DroidSans-Bold.ttf"); } });
        test("paint default no typeface", new F() { public Object get() throws Exception { return null; } });
        System.out.println("[t] done");
    }

    static void test(String name, F f) {
        try {
            Object o = f.get();
            Typeface tf = o instanceof Typeface ? (Typeface) o : Typeface.create("sans-serif", 0);
            Paint p = new Paint();
            p.setTypeface(tf);
            p.setTextSize(48);
            float w = p.measureText("devctl 面板 刷新");
            System.out.println("[t] OK  " + name + "  width=" + w);
        } catch (Throwable t) {
            System.out.println("[t] FAIL " + name + "  " + t.getClass().getSimpleName() + ": " + t.getMessage());
        }
    }
}
