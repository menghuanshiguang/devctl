#include <stdio.h>
#include <stdlib.h>
#include <sys/ptrace.h>
#include <sys/wait.h>
#include <unistd.h>

int main(int argc, char **argv) {
    if (argc < 2) { printf("usage: attach_test <pid>\n"); return 2; }
    int pid = atoi(argv[1]);
    if (ptrace(PTRACE_ATTACH, pid, 0, 0) == -1) { perror("attach"); return 1; }
    printf("ATTACH_OK pid=%d\n", pid);
    int st = 0;
    waitpid(pid, &st, 0);
    ptrace(PTRACE_DETACH, pid, 0, 0);
    printf("DETACH_OK\n");
    return 0;
}
