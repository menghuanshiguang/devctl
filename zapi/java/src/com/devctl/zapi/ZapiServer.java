package com.devctl.zapi;

import java.io.BufferedReader;
import java.io.InputStreamReader;
import java.io.OutputStream;
import java.io.OutputStreamWriter;
import java.io.Writer;
import java.net.InetAddress;
import java.net.InetSocketAddress;
import java.net.ServerSocket;
import java.net.Socket;
import java.nio.charset.Charset;

/**
 * ZapiServer: 宿主进程内的命令服务 (TCP 127.0.0.1:8288)
 * 协议: JSON Lines
 *   请求: {"id":1,"method":"overlay.open","args":{}}     (args 可省)
 *   响应: {"id":1,"ok":true,"data":{...},"stderr":""}
 * devctl client 通过该端口远程调用宿主内任意 API (走 DevBridge.call)。
 */
public class ZapiServer {

    private static final int PORT = 8288;
    private static volatile boolean running = false;

    public static void start() {
        if (running) return;
        running = true;
        Thread t = new Thread(new Runnable() {
            @Override public void run() { serve(); }
        });
        t.setDaemon(true);
        t.start();
    }

    private static void serve() {
        ServerSocket ss = null;
        try {
            ss = new ServerSocket();
            ss.bind(new InetSocketAddress(InetAddress.getByName("127.0.0.1"), PORT), 4);
        } catch (Exception e) {
            running = false;
            return;
        }
        while (running) {
            try {
                Socket s = ss.accept();
                handle(s);
            } catch (Exception e) {}
        }
    }

    private static void handle(final Socket s) {
        try {
            s.setSoTimeout(8000);
            BufferedReader rd = new BufferedReader(new InputStreamReader(s.getInputStream(), "UTF-8"), 8192);
            Writer w = new OutputStreamWriter(s.getOutputStream(), "UTF-8");
            String line;
            while ((line = rd.readLine()) != null) {
                if (line.trim().length() == 0) continue;
                String resp = DevBridge.call(line);
                w.write(resp + "\n");
                w.flush();
            }
            s.close();
        } catch (Exception e) {
            try { s.close(); } catch (Exception e2) {}
        }
    }
}
