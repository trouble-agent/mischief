/* victim: writes one line to a target file, then one line to /dev/null.
   Under the shim the first write must fail EIO and the second must succeed. */
#define _GNU_SOURCE
#include <stdio.h>
#include <string.h>
#include <errno.h>
#include <fcntl.h>
#include <unistd.h>

int main(int argc, char **argv) {
  const char *path = argc > 1 ? argv[1] : "/tmp/mc-victim.txt";
  int f = open(path, O_WRONLY | O_CREAT | O_TRUNC, 0600);
  ssize_t n = write(f, "hello\n", 6);
  printf("write(%s) -> %zd errno=%s\n", path, n, n < 0 ? strerror(errno) : "-");
  close(f);
  int d = open("/dev/null", O_WRONLY);
  ssize_t m = write(d, "hello\n", 6);
  printf("write(/dev/null) -> %zd errno=%s\n", m, m < 0 ? strerror(errno) : "-");
  close(d);
  return 0;
}
