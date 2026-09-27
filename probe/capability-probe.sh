#!/usr/bin/env bash
# mischief — capability probe (draft evidence for the v0.1 PRD)
set -u
echo "== host =="; uname -r; echo "cores=$(nproc)"; echo "uid=$(id -u)"
echo "== cgroup =="; echo "fstype=$(stat -fc %T /sys/fs/cgroup)"; echo "controllers=$(cat /sys/fs/cgroup/cgroup.controllers 2>/dev/null)"; echo "subtree=$(cat /sys/fs/cgroup/cgroup.subtree_control 2>/dev/null)"
echo "== user delegation =="; ls -ld "/sys/fs/cgroup/user.slice/user-$(id -u).slice" 2>&1 | head -1; echo "user_cgroup_controllers=$(cat /sys/fs/cgroup/user.slice/user-$(id -u).slice/cgroup.controllers 2>/dev/null)"
echo "== cgroup create =="; if mkdir /sys/fs/cgroup/mc-probe 2>/dev/null; then echo "cgroup_mkdir=OK"; cat /sys/fs/cgroup/mc-probe/cgroup.controllers 2>/dev/null; rmdir /sys/fs/cgroup/mc-probe 2>/dev/null; else echo "cgroup_mkdir=DENIED(unprivileged)"; fi
echo "== psi =="; ls /proc/pressure/ 2>&1 | tr '\n' ' '; echo; head -1 /proc/pressure/io 2>/dev/null
echo "== sudo =="; sudo -n true 2>/dev/null && echo "passwordless_sudo=OK" || echo "passwordless_sudo=NO"
echo "== tools =="; for t in cc gcc zig tc ip unshare nsenter dmsetup losetup mkfs.ext4 fsfreeze strace jq socat; do printf "%s=%s " "$t" "$(command -v "$t" || echo MISSING)"; done; echo
echo "== freezer =="; grep -qw freezer /sys/fs/cgroup/cgroup.controllers && echo "freezer=present" || echo "freezer=absent-at-root"
echo "== namespaces =="; echo "max_user_ns=$(cat /proc/sys/user/max_user_namespaces 2>/dev/null)"; unshare --time --boottime 1000 true 2>&1 && echo "timens_noroot=OK" || echo "timens_noroot=needs-privilege"
echo "== seccomp =="; grep -E '^Seccomp:' /proc/self/status
echo "== docker =="; docker info --format 'docker={{.ServerVersion}} cgroup={{.CgroupVersion}}' 2>&1 | head -1
echo "== loop/devmapper =="; ls /dev/loop-control /dev/mapper/control 2>&1 | tr '\n' ' '; echo; echo "loop_free=$(losetup -f 2>&1)"
