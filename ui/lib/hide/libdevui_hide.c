// libdevui_hide.so — devui 进程自伪装 (仅操作自身进程, 与 agent hide 同理念)
// JNI_OnLoad 里: comm → "netd", cmdline → "/system/bin/netd"
// 被 devui.dex 入口 System.load() 加载; 伪装失败不致命。
#include <jni.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <fcntl.h>
#include <sys/prctl.h>
#include <sys/mman.h>

#define NEW_NAME "netd"
#define NEW_CMD  "/system/bin/netd"

// 在 mmap 区域内搜索 needle (来自 hide.go 思路: cmdline 位于栈顶 argv 区)
static long find_needle(const char *area, long areaLen, const char *needle, long needleLen) {
    if (area == NULL || needleLen <= 0 || areaLen < needleLen) return -1;
    long lastStart = areaLen - needleLen;
    for (long i = lastStart; i >= 0; i--) {
        if (area[i] == needle[0] && memcmp(area + i, needle, needleLen) == 0) {
            return i;
        }
    }
    return -1;
}

// 覆盖自身 cmdline: 保长不变 (0 填充后写新串), 失败静默
static void disguise_cmdline(void) {
    // 取当前 cmdline
    int fd = open("/proc/self/cmdline", O_RDONLY);
    if (fd < 0) return;
    char cur[512];
    int clen = read(fd, cur, sizeof(cur) - 1);
    close(fd);
    if (clen <= 0) return;
    cur[clen] = 0;

    // 找 [stack] 段
    FILE *maps = fopen("/proc/self/maps", "r");
    if (!maps) return;
    char line[512];
    unsigned long start = 0, end = 0;
    while (fgets(line, sizeof(line), maps)) {
        if (strstr(line, "[stack]") && strstr(line, "rw-p")) {
            sscanf(line, "%lx-%lx", &start, &end);
            break;
        }
    }
    fclose(maps);
    if (start == 0 || end <= start) return;

    long areaLen = (long) (end - start);
    long maxScan = areaLen > 16 * 1024 * 1024 ? 16 * 1024 * 1024 : areaLen; // 最多扫 16MB
    // 只在栈顶 (高地址) 附近找: argv 在栈顶
    long scanOff = areaLen - maxScan;
    if (scanOff < 0) scanOff = 0;

    char *map = (char *) mmap(NULL, maxScan, PROT_READ, MAP_PRIVATE, open("/proc/self/mem", O_RDONLY), 0);
    // /proc/self/mem 用 mmap 不可靠; 改为 pread
    if (map != MAP_FAILED) { munmap(map, maxScan); map = NULL; }

    int mfd = open("/proc/self/mem", O_RDWR);
    if (mfd < 0) return;

    char *buf = (char *) malloc(maxScan);
    if (!buf) { close(mfd); return; }
    if (pread(mfd, buf, maxScan, (off_t) (start + scanOff)) != maxScan) { free(buf); close(mfd); return; }

    long off = find_needle(buf, maxScan, cur, clen);
    if (off >= 0) {
        unsigned long addr = start + scanOff + off;
        // 保长不变: 先 0 填充再写新串
        size_t newLen = strlen(NEW_CMD) + 1;
        if (newLen <= (size_t) clen) {
            char zero[512];
            memset(zero, 0, sizeof(zero));
            pwrite(mfd, zero, clen, (off_t) addr);
            pwrite(mfd, NEW_CMD, newLen, (off_t) addr);
        }
    }
    free(buf);
    close(mfd);
}

JNIEXPORT jint JNICALL JNI_OnLoad(JavaVM *vm, void *reserved) {
    (void) vm; (void) reserved;
    prctl(PR_SET_NAME, NEW_NAME, 0, 0, 0);
    disguise_cmdline();
    return JNI_VERSION_1_6;
}
