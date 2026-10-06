# Capability audit — measured on this host, 2026-10-06

**Transcript provenance (MSF-024).** The first block below is the verbatim stdout of
`bash capability-probe.sh` run from this `probe/` directory on 2026-10-06 —
transcript generated from capability-probe.sh @ `09eaff0b8738` (full:
`git rev-parse HEAD:probe/capability-probe.sh` = `09eaff0b873830586621575caa5e74a9d60f622d`).
If the script is ever edited, the hash above changes and this block stops being
authoritative until regenerated: re-run `cd probe && bash capability-probe.sh` and
diff your output against the block — every difference is then either host drift or
script drift, never stale-doc noise.

The remaining blocks are **manual-run**: mechanism proofs that were compiled and run
by hand the same session. They are NOT produced by the script alone and are labelled
as such. Volatile fields in every block (PSI counters, systemd unit/invocation ids,
netem qdisc handle and seed, pids, timestamps) differ on every run by design.

## Script transcript (verbatim output of capability-probe.sh @ 09eaff0b8738)

```
== host ==
7.0.0-31-generic
cores=16
uid=1000
== cgroup ==
fstype=cgroup2fs
controllers=cpuset cpu io memory hugetlb pids rdma misc dmem
subtree=cpuset cpu io memory hugetlb pids rdma misc dmem
== user delegation ==
drwxr-xr-x+ 6 root root 0 Oct  5 23:48 /sys/fs/cgroup/user.slice/user-1000.slice
user_cgroup_controllers=cpu memory pids
== cgroup create ==
cgroup_mkdir=DENIED(unprivileged)
== psi ==
cpu io memory 
some avg10=1.18 avg60=2.03 avg300=1.47 total=309848436301
== sudo ==
passwordless_sudo=OK
== tools ==
cc=/usr/bin/cc gcc=/usr/bin/gcc zig=MISSING tc=/usr/sbin/tc ip=/usr/sbin/ip unshare=/usr/bin/unshare nsenter=/usr/bin/nsenter dmsetup=/usr/sbin/dmsetup losetup=/usr/sbin/losetup mkfs.ext4=/usr/sbin/mkfs.ext4 fsfreeze=/usr/sbin/fsfreeze strace=/usr/bin/strace jq=/usr/bin/jq socat=/usr/bin/socat 
== freezer ==
freezer=absent-at-root
== namespaces ==
max_user_ns=243115
unshare: unshare failed: Operation not permitted
timens_noroot=needs-privilege
== seccomp ==
Seccomp:	0
== docker ==
docker=29.1.3 cgroup=2
== loop/devmapper ==
/dev/loop-control /dev/mapper/control 
loop_free=/dev/loop27
```

## Mechanism proofs (manual-run: compiled and run by hand, not by the script)

```
$ cc -shared -fPIC -O2 -o libfault-probe.so libfault-probe.c -ldl && cc -O2 -o victim victim.c
(no output; compile OK)

$ cc -O2 -o seccomp-probe seccomp-probe.c && ./seccomp-probe
seccomp_user_notify=SUPPORTED notif=80 resp=24 notif_sizes_probe=OK

$ ./victim /tmp/mc-victim.txt                       # A: control run, no fault
write(/tmp/mc-victim.txt) -> 6 errno=-
write(/dev/null) -> 6 errno=-
-rw------- 1 kara kara 6 Oct  6 05:45 /tmp/mc-victim.txt    # 6 bytes written

$ MCFAULT="write:mc-victim.txt:EIO" LD_PRELOAD=./libfault-probe.so ./victim /tmp/mc-victim.txt
write(/tmp/mc-victim.txt) -> -1 errno=Input/output error   # <-- fault LANDED
write(/dev/null) -> 6 errno=-                              # unaffected fd keeps working
/tmp/mc-victim.txt -> 0 bytes                              # target state proves the fault
/tmp/mc-fault-hits.<pid> -> "hits=1 rule=write:mc-victim.txt:EIO"   # <-- LANDED-PROOF (pid-keyed; this run's file verified to contain exactly that line)
$ MCFAULT="write:mc-victim.txt:EIO:1" LD_PRELOAD=./libfault-probe.so ./victim /tmp/mc-victim.txt
write(/tmp/mc-victim.txt) -> -1 errno=Input/output error   # bounded fault: exactly one hit
```

## Network fault substrate (manual-run: sudo netns + netem, run by hand)

```
$ sudo ip netns add mc-probe-ns                  # network fault substrate
netns_add=OK
$ sudo ip netns exec mc-probe-ns tc qdisc add dev lo root netem loss 100%
netem_loss100=OK
$ sudo ip netns exec mc-probe-ns tc qdisc show dev lo
qdisc netem 8003: root refcnt 2 limit 1000 loss 100% seed 969907942566631981
$ sudo ip netns exec mc-probe-ns timeout 5 ping -c1 127.0.0.1 ; echo rc=$?
rc=2                                             # the fault LANDED (loopback blackholed)
$ sudo ip netns del mc-probe-ns && echo netns_del=OK
netns_del=OK                                     # revert proven in the same minute

$ unshare -Un sh -c 'ip link add veth0 type veth peer name veth1'
RTNETLINK answers: Operation not permitted        # Ubuntu AppArmor userns restriction
```

## Rootless resource limits + freezer (manual-run: run by hand)

```
$ systemd-run --user --scope -p MemoryMax=64M -p CPUQuota=10% -p TasksMax=32 true
Running as unit: run-p3609447-i451630401.scope; invocation ID: 9f5aec068df14d2985e7c5a82e96d067    # rootless resource limits WORK
$ systemctl --user freeze --help >/dev/null && echo user_freeze_verb=present
user_freeze_verb=present                           # systemd 259
$ sudo mkdir /sys/fs/cgroup/mc-probe && ls /sys/fs/cgroup/mc-probe | grep -cE 'cgroup.freeze|memory.max|cpu.max'
4                                                  # a child cgroup (as root) HAS cgroup.freeze
                                                   # (note: the probe script's own == freezer == section
                                                   #  reports freezer=absent-at-root — the ROOT cgroup has no
                                                   #  cgroup.freeze; a root-created CHILD does. Both are true.)
```

## Summary

| capability | state on this host | consequence for the design |
|---|---|---|
| cgroup v2 unified | PROVEN | resource faults are cgroup-shaped |
| rootless resource limits (`systemd-run --user` scope) | PROVEN | L1 tier needs no privileges |
| cgroup mkdir as uid 1000 | DENIED | privileged helper or transient scopes only |
| cgroup freezer | PROVEN as a root-created child cgroup | freeze/hang faults are L2 |
| PSI (cpu/io/memory) | PROVEN | pressure faults and the load gate |
| seccomp user-notify | PROVEN (`notif=80 resp=24`) | syscall error-injection backend |
| LD_PRELOAD fault + landed-proof | PROVEN end-to-end | the flagship rootless primitive and the proof contract |
| netns + netem (+ revert) | PROVEN with sudo | L2 network faults; helper required |
| unprivileged userns network | PARTIAL (`ip link add` EPERM) | docker becomes a first-class backend |
| time namespaces | NOT AVAILABLE rootless | T layer is shim-based, ships last |
| docker | PROVEN (29.1.3, cgroup v2) | container nodes; NET_ADMIN fallback |
| loop / device-mapper | UNPROVEN (tools present) | M2 behind the L2 helper, selftest-gated |

## Conclusions that changed the design

1. **Rootless resource faults are real** (`systemd-run --user --scope` with `MemoryMax`,
   `CPUQuota`, `TasksMax`): the L1 tier needs no privileges at all. My first probe claimed
   `freezer=absent` — **wrong**: freezer is a cgroup v2 core feature, not a controller, and
   the root cgroup has no `cgroup.freeze`; a child cgroup does.
2. **Unprivileged user+net namespaces are effectively unavailable for network faults**
   (`unshare -Un` works, `ip link add` is refused by the host's AppArmor userns
   restriction), so network faults need either the narrow privileged helper or a
   `NET_ADMIN` container. That is why docker is a first-class backend, not an extra.
3. **Time namespaces need privilege** (`unshare --time` → `EPERM`), so the T layer is
   shim-based in v0.1 and ships last.
4. **seccomp user-notify is present** (`notif=80 resp=24`), so syscall error injection
   has a rootless backend alongside the shim.
5. **The landed-proof contract is implementable as designed** — the shim proved a fault
   landed and wrote an independent counter file, in the same run that the control had no
   counter. This is the mechanism the whole design is built on.
6. `zig` is missing on this host (relevant to any cross-compiled `.so` release step).
