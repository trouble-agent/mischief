#include <errno.h>
#include <fcntl.h>
#include <signal.h>
#include <stdio.h>
#include <string.h>
#include <unistd.h>

int main(int argc, char **argv) {
  if (argc != 3) return 2;
  const char *path = argv[1], *mode = argv[2];
#ifdef SWALLOW
  const int handles = 0;
#else
  const int handles = 1;
#endif
  if (!strcmp(mode, "epipe")) {
    int p[2]; if (pipe(p)) return 2;
    signal(SIGPIPE, SIG_IGN); close(p[0]);
    ssize_t n = write(p[1], "x", 1); int fault = n < 0 && errno == EPIPE;
    close(p[1]); printf("fault=%d handled=%d\n", fault, handles && fault); return 0;
  }
  if (!strcmp(mode, "partialstdin")) {
    char buf[64]; ssize_t n = read(0, buf, sizeof(buf)); if (n < 0) return 2;
    int fd = open(path, O_WRONLY | O_CREAT | O_TRUNC, 0600); if (fd < 0) return 2;
    if (handles && write(fd, buf, n) != n) return 2;
    ssize_t next = read(0, buf, sizeof(buf));
    close(fd);
    printf("fault=%d handled=%d bytes=%zd eof=%d\n", n == 7 && next == 0, handles && n == 7 && next == 0, n, next == 0); return 0;
  }
  int fd = open(path, O_WRONLY | O_CREAT | O_TRUNC, 0600);
  if (fd < 0) { printf("fault=%d handled=%d\n", errno == EACCES, handles && errno == EACCES); return 0; }
  int attempts = 0, fault = 0; ssize_t n;
  do { n = write(fd, "abcdef", 6); attempts++; if (n < 0 && errno == EINTR) fault = 1; } while (handles && n < 0 && errno == EINTR);
  if (n < 0 && errno == ENOSPC) fault = 1;
  if (!strcmp(mode, "control")) fault = 0;
  close(fd);
  printf("fault=%d handled=%d attempts=%d\n", fault, handles && fault, attempts);
  return 0;
}
