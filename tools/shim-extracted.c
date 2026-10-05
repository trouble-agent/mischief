/* mischief LD_PRELOAD fault shim — GENERATED, anchored (SPEC-06).
 * Rule (env): MCFAULT=sys:pattern:errno  (exactly 3 slots, greedy middle)
 * Budget:     MCFAULT_BUDGET=N           (absent = unlimited)
 * Proof:      MCFAULT_PROOF=path         (per-pid counter, self-excluded)
 * Anchoring:  pattern ends ":*"  -> prefix: the dir itself or under it
 *             pattern starts "*:"-> suffix: the name or /name
 *             otherwise          -> exact match
 * This is NOT the legacy probe shim: no bare substring, no self-matching
 * proof channel (MSF-028). */
#define _GNU_SOURCE
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <dlfcn.h>
#include <errno.h>
#include <unistd.h>
#include <fcntl.h>

static ssize_t (*real_write)(int, const void *, size_t) = NULL;
static char rule_sys[64], rule_pattern[512], rule_errno[32];
static char proof_path[512];
static int rule_budget = -1, hits = 0, loaded = 0;

static int errno_for(const char *name) {
  if (!strcmp(name, "EIO")) return EIO;
  if (!strcmp(name, "ENOSPC")) return ENOSPC;
  if (!strcmp(name, "EACCES")) return EACCES;
  if (!strcmp(name, "EDQUOT")) return EDQUOT;
  if (!strcmp(name, "ENOMEM")) return ENOMEM;
  if (!strcmp(name, "EINTR")) return EINTR;
  if (!strcmp(name, "ETIMEDOUT")) return ETIMEDOUT;
  if (!strcmp(name, "EAGAIN")) return EAGAIN;
  if (!strcmp(name, "EPIPE")) return EPIPE;
  return EIO;
}

static void landproof(void) {
  if (!proof_path[0]) return;
  int f = open(proof_path, O_WRONLY | O_CREAT | O_TRUNC, 0600);
  if (f >= 0) {
    dprintf(f, "hits=%d rule=%s:%s:%s\n", hits, rule_sys, rule_pattern, rule_errno);
    close(f);
  }
}

/* anchored target match: exact / prefix "dir:*" / suffix "*:name",
   path-boundary at '/'; the proof path NEVER matches (self-exclusion). */
static int pattern_matches(const char *tgt) {
  size_t plen = strlen(rule_pattern);
  if (plen >= 2 && !strcmp(rule_pattern + plen - 2, ":*")) {
    size_t alen = plen - 2;
    if (strncmp(tgt, rule_pattern, alen) != 0) return 0;
    return tgt[alen] == '\0' || tgt[alen] == '/';
  }
  if (plen >= 2 && rule_pattern[0] == '*' && rule_pattern[1] == ':') {
    const char *name = rule_pattern + 2;
    size_t nlen = plen - 2;
    size_t tlen = strlen(tgt);
    if (tlen < nlen) return 0;
    if (strcmp(tgt + tlen - nlen, name) != 0) return 0;
    return tlen == nlen || tgt[tlen - nlen - 1] == '/';
  }
  return strcmp(tgt, rule_pattern) == 0;
}

static int target_fd_matches(int fd) {
  char link[64], tgt[1024];
  snprintf(link, sizeof link, "/proc/self/fd/%d", fd);
  ssize_t n = readlink(link, tgt, sizeof tgt - 1);
  if (n <= 0) return 0;
  tgt[n] = 0;
  if (proof_path[0] && strcmp(tgt, proof_path) == 0) return 0;
  return pattern_matches(tgt);
}

ssize_t write(int fd, const void *buf, size_t n) {
  if (!real_write) real_write = dlsym(RTLD_NEXT, "write");
  if (loaded && !strcmp(rule_sys, "write") && target_fd_matches(fd)) {
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
  if (loaded) return;
  const char *r = getenv("MCFAULT");
  if (!r) return;
  loaded = 1;
  /* exactly three slots: sys, greedy middle = pattern, errno = last */
  const char *first = strchr(r, ':');
  const char *last = strrchr(r, ':');
  if (!first || last == first) return;
  size_t syslen = (size_t)(first - r);
  size_t patlen = (size_t)(last - first - 1);
  size_t errlen = strlen(last + 1);
  if (syslen >= sizeof rule_sys || patlen >= sizeof rule_pattern ||
      errlen >= sizeof rule_errno) return;
  memcpy(rule_sys, r, syslen); rule_sys[syslen] = 0;
  memcpy(rule_pattern, first + 1, patlen); rule_pattern[patlen] = 0;
  memcpy(rule_errno, last + 1, errlen); rule_errno[errlen] = 0;
  const char *b = getenv("MCFAULT_BUDGET");
  if (b && *b) rule_budget = atoi(b);
  const char *p = getenv("MCFAULT_PROOF");
  if (p && *p) { snprintf(proof_path, sizeof proof_path, "%s", p); }
  real_write = dlsym(RTLD_NEXT, "write");
}
