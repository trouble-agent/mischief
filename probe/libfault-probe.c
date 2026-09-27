/* mischief — LD_PRELOAD fault-injection shim: minimal PROOF OF MECHANISM.
 *
 * Rule form used by the probe: MCFAULT="write:<path-substring>:EIO[:count]"
 * The shim counts every rule HIT into a counter file (the LANDPROOF) and can
 * fail a syscall with a chosen errno. Written to prove two things live:
 *   1. a fault can be landed rootless, inside an arbitrary process;
 *   2. the fault can PROVE IT LANDED (independent counter) — the "no-op vs
 *      negative result" distinction the real design is built around.
 */
#define _GNU_SOURCE
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <dlfcn.h>
#include <errno.h>
#include <unistd.h>
#include <fcntl.h>

static ssize_t (*real_write)(int, const void *, size_t) = NULL;
static char rule_sys[32], rule_path[256], rule_errno[32];
static int rule_budget = -1, hits = 0, loaded = 0;

static int errno_for(const char *name) {
  if (!strcmp(name, "EIO")) return EIO;
  if (!strcmp(name, "ENOSPC")) return ENOSPC;
  if (!strcmp(name, "EACCES")) return EACCES;
  if (!strcmp(name, "EDQUOT")) return EDQUOT;
  if (!strcmp(name, "ENOMEM")) return ENOMEM;
  if (!strcmp(name, "EINTR")) return EINTR;
  if (!strcmp(name, "ETIMEDOUT")) return ETIMEDOUT;
  return EIO;
}

static void landproof(void) {
  char p[128];
  snprintf(p, sizeof p, "/tmp/mc-fault-hits.%d", (int)getpid());
  int f = open(p, O_WRONLY | O_CREAT | O_TRUNC, 0600);
  if (f >= 0) { dprintf(f, "hits=%d rule=%s:%s:%s\n", hits, rule_sys, rule_path, rule_errno); close(f); }
}

static int target_fd_matches(int fd) {
  char link[64], tgt[512];
  snprintf(link, sizeof link, "/proc/self/fd/%d", fd);
  ssize_t n = readlink(link, tgt, sizeof tgt - 1);
  if (n <= 0) return 0;
  tgt[n] = 0;
  return strstr(tgt, rule_path) != NULL;
}

ssize_t write(int fd, const void *buf, size_t n) {
  if (!real_write) real_write = dlsym(RTLD_NEXT, "write");
  if (!strcmp(rule_sys, "write") && target_fd_matches(fd)) {
    if (rule_budget < 0 || hits < rule_budget) {
      hits++;
      landproof();
      errno = errno_for(rule_errno);
      return -1;
    }
  }
  return real_write(fd, buf, n);
}

__attribute__((constructor)) static void init(void) {
  const char *r = getenv("MCFAULT");
  if (!r || loaded) return;
  loaded = 1;
  char tmp[512];
  snprintf(tmp, sizeof tmp, "%s", r);
  char *a = strtok(tmp, ":"), *b = strtok(NULL, ":"), *c = strtok(NULL, ":"), *d = strtok(NULL, ":");
  if (a) snprintf(rule_sys, sizeof rule_sys, "%s", a);
  if (b) snprintf(rule_path, sizeof rule_path, "%s", b);
  if (c) snprintf(rule_errno, sizeof rule_errno, "%s", c);
  rule_budget = d ? atoi(d) : -1;
  real_write = dlsym(RTLD_NEXT, "write");
}
