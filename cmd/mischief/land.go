package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/trouble-agent/mischief/internal/reverter"
)

// cmdLand records that the CALLER landed the fault on an armed hold.
//
// The documented loop is:
//
//	declare → plan → arm → LAND → prove landed → hold → observe → revert → grade
//
// and `arm` is write-ahead only: it records the hold and stops there. Until
// this verb existed the loop had no way to say the fault was applied, so the
// journal could not distinguish "injected and recovered" from "armed, nothing
// happened, TTL expired" — and the second case published a green `reverted`
// proof. A consumer reading that would conclude its code survived a fault it
// was never exposed to.
//
// What this verb does NOT do is prove anything. It records the operator's own
// claim, which is why the loop keeps "prove landed" as a separate step: an
// independent witness (syscall trace, filesystem fact, process exit status)
// must corroborate that the fault arrived. This verb alone is not evidence.
func cmdLand(args []string) int {
	fs := flag.NewFlagSet("land", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	dir := fs.String("dir", defaultRunDir(), "run dir (journal home)")
	hold := fs.String("hold", "", "hold id to land (required unless --all)")
	all := fs.Bool("all", false, "land every currently-armed hold")
	if err := fs.Parse(args); err != nil {
		return verbParseExit(err)
	}
	if *hold == "" && !*all {
		fmt.Fprintln(os.Stderr, "land: --hold <id> or --all required (landing nothing is not a land)")
		return 2
	}
	if *hold != "" && *all {
		fmt.Fprintln(os.Stderr, "land: --hold and --all are mutually exclusive")
		return 2
	}

	store, err := reverter.Open(*dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "land:", err)
		return 1
	}
	defer store.Close()
	lf := reverter.New(store)

	if *hold != "" {
		if err := lf.Land(*hold); err != nil {
			fmt.Fprintln(os.Stderr, "land:", err)
			return 1
		}
		fmt.Printf("landed %s\n", *hold)
		return 0
	}

	// --all: land every armed hold, the way a batch injector does after it has
	// applied each fault in turn.
	n := 0
	for _, h := range lf.StatusAll().Holds {
		if h.Status != reverter.StatusArmed {
			continue
		}
		if err := lf.Land(h.HoldID); err != nil {
			fmt.Fprintf(os.Stderr, "land %s: %v\n", h.HoldID, err)
			return 1
		}
		fmt.Printf("landed %s fault=%s target=%s\n", h.HoldID, h.FaultID, h.Target)
		n++
	}
	if n == 0 {
		fmt.Fprintln(os.Stderr, "land: no armed holds to land")
		return 1
	}
	fmt.Printf("landed %d hold(s)\n", n)
	return 0
}
