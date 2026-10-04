package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/trouble-agent/mischief/internal/catalog"
)

// hostCapabilityRegistry is the AC-11 seam's production implementation: it
// answers the catalog's capability kinds from live host reads, fail-closed
// (an unreadable host reads as INCAPABLE, never capable — the same rule
// backend_resource's probes encode).
//
// M1 keeps the set small and measured:
//
//   - "none" is never asked of the registry (catalog.Status short-circuits);
//   - docker → the docker socket answers on /var/run/docker.sock;
//   - cgroup2 → the v2 mount is present (cgroup.controllers exists);
//   - NET_ADMIN → denied for an unprivileged M1 process (fail closed:
//     the L2 helper does not exist yet, so the honest answer is no);
//   - any other kind → unavailable, and doctor/plan NAME it (AC-11's
//     "naming what is absent").
func hostCapabilityRegistry() catalog.CapabilityRegistry {
	return catalog.RegistryFunc(func(kind string) bool {
		switch {
		case kind == "docker" || strings.HasPrefix(kind, "docker"):
			return dockerSocketPresent()
		case kind == "cgroup2" || strings.HasPrefix(kind, "cgroup2"):
			return fileExists("/sys/fs/cgroup/cgroup.controllers")
		case kind == "NET_ADMIN":
			return false // L2 privileged helper: not shipped in M1 (fail closed)
		default:
			return false
		}
	})
}

func dockerSocketPresent() bool {
	fi, err := os.Stat("/var/run/docker.sock")
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeSocket != 0
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

var _ = fmt.Sprintf // keep fmt available for future registry notes
