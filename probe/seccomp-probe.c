/* mischief — seccomp user-notify support probe (SECCOMP_GET_NOTIF_SIZES). */
#define _GNU_SOURCE
#include <stdio.h>
#include <string.h>
#include <errno.h>
#include <sys/syscall.h>
#include <unistd.h>
#include <linux/seccomp.h>

#ifndef SECCOMP_GET_NOTIF_SIZES
#define SECCOMP_GET_NOTIF_SIZES 3
#endif

int main(void) {
  struct seccomp_notif_sizes s;
  memset(&s, 0, sizeof s);
  long rc = syscall(SYS_seccomp, SECCOMP_GET_NOTIF_SIZES, 0, &s);
  if (rc != 0) { printf("seccomp_user_notify=UNSUPPORTED errno=%s\n", strerror(errno)); return 1; }
  printf("seccomp_user_notify=SUPPORTED notif=%zu resp=%zu notif_sizes_probe=OK\n",
         s.seccomp_notif, s.seccomp_notif_resp);
  return 0;
}
