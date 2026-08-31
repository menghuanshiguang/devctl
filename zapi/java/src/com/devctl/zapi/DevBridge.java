package com.devctl.zapi;

import android.content.Context;
import android.os.Build;

import org.json.JSONObject;

import java.lang.reflect.Method;

/**
 * DevBridge: 动态 API 调用调度层 (被注入端加载的入口类)
 *
 * 调用链:
 *   native (libdevui.so) → 反射 ActivityThread.currentApplication() 拿 Context
 *                         → DexClassLoader 加载本 dex → DevBridge.bootstrap(ctx)
 *   ZapiServer (TCP)     → DevBridge.call(jsonLine) → 命令表分发
 *
 * 命令表是"动态 API"的地基: 后续任何宿主内能力 (悬浮窗/输入/剪贴板/...) 都
 * 通过注册到这里的方法暴露, client 侧零改动。
 */
public class DevBridge {

    private static Context appCtx;

    /** native 注入端调用: 引导入口 */
    public static void bootstrap(Context ctx) {
        appCtx = ctx.getApplicationContext();
        ZapiServer.start();
        InjectedOverlay.create(appCtx);
    }

    /** ZapiServer 调用: 处理一行 JSON 请求, 返回 JSON 响应行 */
    public static String call(String jsonLine) {
        JSONObject resp = new JSONObject();
        try {
            JSONObject req = new JSONObject(jsonLine);
            long id = req.optLong("id", 0);
            String method = req.optString("method", "");
            JSONObject args = req.optJSONObject("args");
            if (args == null) args = new JSONObject();

            Object result = dispatch(method, args);
            resp.put("id", id);
            resp.put("ok", true);
            resp.put("data", result == null ? new JSONObject() : result);
        } catch (Exception e) {
            try {
                resp.put("ok", false);
                resp.put("stderr", e.toString());
            } catch (Exception e2) {}
        }
        return resp.toString();
    }

    // ==================== 命令表 ("动态 API" 核心) ====================

    private static Object dispatch(String method, JSONObject args) throws Exception {
        if (method == null) throw new IllegalArgumentException("no method");
        switch (method) {
            // ---- 系统信息 ----
            case "sys.info": return sysInfo();
            // ---- 悬浮控件 ----
            case "overlay.open":  InjectedOverlay.get().openPanel(); return ok();
            case "overlay.close": InjectedOverlay.get().closePanel(); return ok();
            case "overlay.refresh": InjectedOverlay.get().refresh(); return ok();
            case "overlay.destroy": InjectedOverlay.destroy(); return ok();
            case "overlay.status": {
                JSONObject o = new JSONObject();
                o.put("alive", InjectedOverlay.get() != null);
                return o;
            }
            // ---- 通用反射调用 (真正意义的"动态调用 API") ----
            // args: {clazz:"android.os.SystemProperties", method:"get", params:["ro.product.model"], paramsCls:["java.lang.String"]}
            case "java.call": return javaCall(args);
            default:
                throw new IllegalArgumentException("unknown method: " + method);
        }
    }

    private static JSONObject ok() { return new JSONObject(); }

    private static JSONObject sysInfo() throws Exception {
        JSONObject o = new JSONObject();
        o.put("android", Build.VERSION.RELEASE);
        o.put("sdk", Build.VERSION.SDK_INT);
        o.put("host", "injected");
        return o;
    }

    /**
     * java.call: 任意静态方法反射调用, 参数值仅支持 string/int/long/bool/json
     * 示例: {"method":"java.call","args":{"clazz":"android.os.Build","method":"getString","params":["ro.product.model"],"paramsCls":["java.lang.String"]}}
     */
    private static Object javaCall(JSONObject a) throws Exception {
        String clazz = a.getString("clazz");
        String methodName = a.getString("method");
        Object[] params = jsonToArr(a.optJSONArray("params"));
        Class<?>[] pc = jsonToClsArr(a.optJSONArray("paramsCls"));
        Class<?> c = Class.forName(clazz, true, DevBridge.class.getClassLoader());
        Method m = c.getMethod(methodName, pc);
        Object r = m.invoke(null, params);
        return r instanceof String ? new JSONObject().put("value", r) : (Object) new JSONObject().put("value", String.valueOf(r));
    }

    private static Object[] jsonToArr(org.json.JSONArray arr) throws Exception {
        if (arr == null) return new Object[0];
        Object[] out = new Object[arr.length()];
        for (int i = 0; i < arr.length(); i++) {
            Object v = arr.get(i);
            if (v instanceof String) out[i] = v;
            else if (v instanceof Integer || v instanceof Long || v instanceof Boolean) out[i] = v;
            else if (v instanceof JSONObject) {
                JSONObject jo = (JSONObject) v;
                if (jo.has("string")) out[i] = jo.getString("string");
                else out[i] = v.toString();
            } else out[i] = v.toString();
        }
        return out;
    }

    private static Class<?>[] jsonToClsArr(org.json.JSONArray arr) throws Exception {
        if (arr == null) return new Class<?>[0];
        Class<?>[] out = new Class<?>[arr.length()];
        for (int i = 0; i < arr.length(); i++) {
            String n = arr.getString(i);
            if ("int".equals(n)) out[i] = int.class;
            else if ("long".equals(n)) out[i] = long.class;
            else if ("boolean".equals(n)) out[i] = boolean.class;
            else if ("String".equals(n)) out[i] = String.class;
            else out[i] = Class.forName(n, true, DevBridge.class.getClassLoader());
        }
        return out;
    }
}
