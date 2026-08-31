// GenFont: 在 build 机(Windows, JDK)上生成点阵字库 devfont.bin
// 格式: [UTF length(2B) + UTF8 字符表] + 每字符 CELL*CELL/8 字节 (1bit, 行优先)
import java.awt.*;
import java.awt.image.*;
import java.io.*;
import javax.imageio.ImageIO;

public class GenFont {
    static final int CELL = 64;

    public static void main(String[] args) throws Exception {
        System.setProperty("java.awt.headless", "true");
        String ascii = "";
        for (char c = 32; c < 127; c++) ascii += c;
        String cn = "面板刷新收起退出型号系统内存架构设备正在连接失败运行状态版本内核点击拖动悬浮控制加载功耗温度网络代理日志管理设置在线离线关闭开始停止重启手机处理器存储电量信号时间日期共个十百千万兆GKKB点杠杠中英文大小写数字";
        // 去重
        StringBuilder sb = new StringBuilder();
        for (char c : (ascii + cn).toCharArray())
            if (sb.indexOf(String.valueOf(c)) < 0) sb.append(c);
        String chars = sb.toString();
        System.out.println("字符数: " + chars.length());

        GraphicsEnvironment ge = GraphicsEnvironment.getLocalGraphicsEnvironment();
        String[] prefer = {"Microsoft YaHei", "SimHei", "SimSun", "Dialog"};
        String fam = null;
        for (String p : prefer) {
            for (String av : ge.getAvailableFontFamilyNames()) {
                if (av.equalsIgnoreCase(p)) { fam = p; break; }
            }
            if (fam != null) break;
        }
        if (fam == null) fam = "Dialog";
        System.out.println("字体: " + fam);
        Font font = new Font(fam, Font.PLAIN, 52);

        DataOutputStream out = new DataOutputStream(new BufferedOutputStream(new FileOutputStream("devfont.bin")));
        out.writeUTF(chars);
        byte[] row = new byte[CELL / 8 * CELL];
        for (int i = 0; i < chars.length(); i++) {
            BufferedImage img = new BufferedImage(CELL, CELL, BufferedImage.TYPE_INT_ARGB);
            Graphics2D g = img.createGraphics();
            g.setRenderingHint(RenderingHints.KEY_TEXT_ANTIALIASING, RenderingHints.VALUE_TEXT_ANTIALIAS_ON);
            g.setFont(font);
            g.setColor(Color.WHITE);
            String s = String.valueOf(chars.charAt(i));
            FontMetrics fm = g.getFontMetrics();
            int x = (CELL - fm.stringWidth(s)) / 2;
            int y = (CELL - fm.getHeight()) / 2 + fm.getAscent();
            g.drawString(s, x, y);
            g.dispose();
            int bi = 0;
            for (int yy = 0; yy < CELL; yy++) {
                int bits = 0;
                for (int xx = 0; xx < CELL; xx++) {
                    bits = (bits << 1) | ((img.getRGB(xx, yy) >>> 24) > 96 ? 1 : 0);
                    if ((xx & 7) == 7) { row[bi++] = (byte) bits; bits = 0; }
                }
            }
            out.write(row);
        }
        out.close();
        System.out.println("OK devfont.bin size=" + new File("devfont.bin").length() + " chars=" + chars.length());
        // 打印字符表供核对
        FileWriter fw = new FileWriter("devfont.chars.txt");
        fw.write(chars);
        fw.close();

        // 预览图: 20 列网格画出全部字符
        int cols = 20, rows = (chars.length() + cols - 1) / cols;
        BufferedImage prev = new BufferedImage(cols * CELL, rows * CELL, BufferedImage.TYPE_INT_RGB);
        Graphics2D g2 = prev.createGraphics();
        g2.setColor(Color.WHITE);
        g2.fillRect(0, 0, prev.getWidth(), prev.getHeight());
        g2.setFont(font);
        g2.setColor(Color.BLACK);
        for (int i = 0; i < chars.length(); i++) {
            int cx = (i % cols) * CELL, cy = (i / cols) * CELL;
            String s = String.valueOf(chars.charAt(i));
            FontMetrics fm = g2.getFontMetrics();
            g2.drawString(s, cx + (CELL - fm.stringWidth(s)) / 2, cy + (CELL - fm.getHeight()) / 2 + fm.getAscent());
        }
        g2.dispose();
        ImageIO.write(prev, "png", new File("devfont_preview.png"));
        System.out.println("preview saved");
    }
}
