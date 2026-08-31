// libdevui.so — 注入载荷: 宿主进程内动态调用 API 的引导层
// 被注入器 dlopen 后调用 devui_entry(dexAddr, dexSize); 职责:
//   1. 等宿主 Application 就绪
//   2. JNI 反射 ActivityThread.currentApplication() 拿 Context
//   3. InMemoryDexClassLoader 从注入器直传的内存字节流加载 dex (零落盘)
//   4. 调 DevBridge.bootstrap(ctx) → 后续一切走 Java 侧 (ZapiServer/DevBridge)
#include <jni.h>
#include <pthread.h>
#include <stdio.h>
#include <string.h>
#include <unistd.h>
#include <dlfcn.h>
#include <android/log.h>

#define LOG_TAG "devui"
#define LOGI(...) __android_log_print(ANDROID_LOG_INFO, LOG_TAG, __VA_ARGS__)

#define BOOTSTRAP_CLASS "com.devctl.zapi.DevBridge"

extern "C" __attribute__((visibility("default"))) void devui_entry(void *dexAddr, int dexSize);

// dex 直传区 (由 devui_entry 设置, init_thread 使用)
static void *devui_dex_addr = NULL;
static int devui_dex_size = 0;

typedef jint (*GetCreatedJavaVMs_t)(JavaVM**, jsize, jsize*);

static void *init_thread(void *arg) {
    (void) arg;
    GetCreatedJavaVMs_t get_vms = NULL;
    JavaVM *vm = NULL;
    jsize n = 0;
    JNIEnv *env = NULL;
    jclass atClass = NULL, imdCls = NULL, uiCls = NULL, db = NULL;
    jclass appCls = NULL;
    jmethodID curApp = NULL, imdCtor = NULL, getCl = NULL, loadCls = NULL, bootstrap = NULL;
    jobject app = NULL, appLoader = NULL, loader = NULL, buffer = NULL;
    jstring clsName = NULL;
    int i = 0;

    // ---- 1. 拿到 JVM ----
    get_vms = (GetCreatedJavaVMs_t) dlsym(RTLD_DEFAULT, "JNI_GetCreatedJavaVMs");
    if (!get_vms) { LOGI("no JNI_GetCreatedJavaVMs"); goto out; }
    if (get_vms(&vm, 1, &n) != 0 || !vm) { LOGI("no java vm"); goto out; }
    if (vm->AttachCurrentThread(&env, NULL) != 0 || !env) { LOGI("attach fail"); goto out; }

    // ---- 2. 等 Application 就绪 ----
    atClass = env->FindClass("android/app/ActivityThread");
    if (!atClass) { LOGI("no ActivityThread"); goto out; }
    curApp = env->GetStaticMethodID(atClass, "currentApplication", "()Landroid/app/Application;");
    if (!curApp) { LOGI("no currentApplication"); goto out; }
    for (i = 0; i < 60; i++) {
        app = env->CallStaticObjectMethod(atClass, curApp);
        if (app != NULL) break;
        usleep(500 * 1000);
    }
    if (!app) { LOGI("app not ready after 30s"); goto out; }
    LOGI("app ready");

    // ---- 3. InMemoryDexClassLoader 从内存加载 (零落盘) ----
    // InMemoryDexClassLoader(ByteBuffer, ClassLoader)
    imdCls = env->FindClass("dalvik/system/InMemoryDexClassLoader");
    if (!imdCls) {
        // API < 29 没有 InMemoryDexClassLoader; 目标设备 Android 16, 理论必有
        LOGI("no InMemoryDexClassLoader");
        goto out;
    }
    imdCtor = env->GetMethodID(imdCls, "<init>",
        "(Ljava/nio/ByteBuffer;Ljava/lang/ClassLoader;)V");
    if (!imdCtor) { LOGI("no imd ctor"); goto out; }

    appCls = env->GetObjectClass(app);
    getCl = env->GetMethodID(appCls, "getClassLoader", "()Ljava/lang/ClassLoader;");
    appLoader = env->CallObjectMethod(app, getCl);

    // dexAddr/dexSize 由 init_arg 保存 (devui_entry 传入)
    jobject buffer = env->NewDirectByteBuffer(devui_dex_addr, devui_dex_size);
    if (!buffer) { LOGI("NewDirectByteBuffer fail"); goto out; }
    loader = env->NewObject(imdCls, imdCtor, buffer, appLoader);
    if (!loader) {
        LOGI("InMemoryDexClassLoader ctor fail");
        if (env->ExceptionCheck()) env->ExceptionDescribe();
        goto out;
    }

    // ---- 4. loadClass + bootstrap ----
    uiCls = env->FindClass("java/lang/ClassLoader");
    loadCls = env->GetMethodID(uiCls, "loadClass", "(Ljava/lang/String;)Ljava/lang/Class;");
    clsName = env->NewStringUTF(BOOTSTRAP_CLASS);
    db = (jclass) env->CallObjectMethod(loader, loadCls, clsName);
    if (!db) {
        LOGI("loadClass fail: %s", BOOTSTRAP_CLASS);
        if (env->ExceptionCheck()) env->ExceptionDescribe();
        goto out;
    }
    bootstrap = env->GetStaticMethodID(db, "bootstrap", "(Landroid/content/Context;)V");
    if (!bootstrap) { LOGI("no bootstrap method"); goto out; }
    env->CallStaticVoidMethod(db, bootstrap, app);
    if (env->ExceptionCheck()) { env->ExceptionDescribe(); goto out; }
    LOGI("bootstrap done");

out:
    if (vm != NULL && env != NULL) {
        vm->DetachCurrentThread();
    }
    return NULL;
}

// 注入器远程调用入口 (注入线程上下文): 立刻派生工作线程, 避免阻塞/污染注入线程
// 参数: x0=dex 内存地址 (目标进程内 mmap 区), x1=dex 大小
extern "C" void devui_entry(void *dexAddr, int dexSize) {
    LOGI("devui_entry called dex=%p size=%d", dexAddr, dexSize);
    devui_dex_addr = dexAddr;
    devui_dex_size = dexSize;
    pthread_t t;
    pthread_create(&t, NULL, init_thread, NULL);
    pthread_detach(t);
}
