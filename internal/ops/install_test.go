package ops

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// MISCHIEF-DF-50: the install --user value is interpolated VERBATIM into
// the sudoers drop-in's final grant line, so a crafted value is a sudoers
// injection — `--user "mischief ALL=(root) NOPASSWD: MISCHIEF_CMNDS\nkara
// ALL=(root) NOPASSWD: ALL #"` renders a second grant line granting kara
// full passwordless root (the trailing template text is neutralized by the
// embedded '#'), and visudo answers "parsed OK" because it checks SYNTAX,
// not intent. ValidateSudoersUser gates the grantee BEFORE interpolation.

// TestValidateSudoersUserAcceptsValidUsernames: the allowlist — plain
// account names [A-Za-z0-9_-], 1..32 chars — plus the empty-default
// contract (InstallOpts.defaults fills "" with defaultSudoersUser BEFORE
// validation, so validation never sees empty in the plan path).
func TestValidateSudoersUserAcceptsValidUsernames(t *testing.T) {
	for _, u := range []string{
		defaultSudoersUser, // mischief
		"a",                // 1 char (min)
		"svc-ops",
		"svc_ops",
		"Kara-T",
		"0operator",
		"-" + strings.Repeat("a", 31), // 32 chars (max)
	} {
		if err := ValidateSudoersUser(u); err != nil {
			t.Fatalf("ValidateSudoersUser(%q) refused a valid username: %v", u, err)
		}
	}
	var ie *InstallError
	if err := ValidateSudoersUser(""); err == nil || !errors.As(err, &ie) {
		t.Fatalf("empty grantee not refused as InstallError: %v", err)
	} else if !strings.Contains(err.Error(), "empty --user") {
		t.Fatalf("empty-grantee refusal does not name the cause: %v", err)
	}
}

// TestValidateSudoersUserRefusesInjectionTable: one row per refusal class
// from the acceptance criteria. Every refusal must be a named InstallError.
func TestValidateSudoersUserRefusesInjectionTable(t *testing.T) {
	tcs := []struct {
		name string
		user string
	}{
		// the board row's live exploit, verbatim
		{"board-row exploit", "mischief ALL=(root) NOPASSWD: MISCHIEF_CMNDS\nkara ALL=(root) NOPASSWD: ALL #"},
		// newlines / carriage returns
		{"newline", "mischief\nkara"},
		{"carriage return", "mischief\rkara"},
		{"trailing newline", "mischief\n"},
		// shell / sudoers metacharacters
		{"semicolon", "mischief;reboot"},
		{"pipe", "mischief|reboot"},
		{"ampersand", "mischief&reboot"},
		{"dollar", "mischief$PATH"},
		{"backtick", "mischief`id`"},
		{"open paren", "mischief(root)"},
		{"close paren", "mischief)"},
		{"less-than", "mischief<file"},
		{"greater-than", "mischief>file"},
		// sudoers keywords (exact tokens)
		{"ALL uppercase", "ALL"},
		{"all lowercase", "all"},
		{"all mixed case", "All"},
		{"NOPASSWD", "NOPASSWD"},
		{"nopasswd lowercase", "nopasswd"},
		{"PASSWD", "PASSWD"},
		{"Cmnd_Alias", "Cmnd_Alias"},
		{"User_Alias", "User_Alias"},
		{"Host_Alias", "Host_Alias"},
		{"Runas_Alias", "Runas_Alias"},
		// whitespace (a single space included — the grantee is one token)
		{"single space", "mischief user"},
		{"leading space", " mischief"},
		{"trailing space", "mischief "},
		{"tab", "mischief\tuser"},
		{"vertical tab", "mischief\vuser"},
		{"form feed", "mischief\fuser"},
		{"non-breaking space", "mischief\u00a0user"},
		// shape violations the allowlist catches anyway
		{"33 characters", strings.Repeat("a", 33)},
		{"equals sign", "mischief=ops"},
		{"colon", "mischief:ops"},
		{"comma", "mischief,ops"},
		{"exclamation", "mischief!ops"},
		{"percent", "mischief%group"},
		{"plus", "mischief+ops"},
		{"at sign", "mischief@host"},
		{"slash", "mischief/ops"},
		{"double quote", `"mischief"`},
		{"hash", "mischief#comment"},
		{"asterisk", "mischief*"},
		{"question mark", "mischief?"},
		{"bracket", "mischief[0]"},
		{"brace", "mischief{}"},
		{"unicode letter", "mischiefé"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateSudoersUser(tc.user)
			if err == nil {
				t.Fatalf("ValidateSudoersUser(%q) accepted a poisoned value", tc.user)
			}
			var ie *InstallError
			if !errors.As(err, &ie) {
				t.Fatalf("refusal is not an InstallError: %T %v", err, err)
			}
			if !strings.Contains(err.Error(), DefaultDropInPath) {
				t.Fatalf("refusal does not name the drop-in path: %v", err)
			}
		})
	}
}

// TestValidateSudoersUserPlanWiring: PlanInstall refuses EVERY refusal
// class (ALL, keyword, metacharacter, line break, whitespace) —
// validation is wired before interpolation, not bolted beside it.
// Empty is deliberately NOT in the plan-path refusal table: defaults()
// fills "" with defaultSudoersUser BEFORE validation (the CLI's
// documented `--user ""` = default contract), and the validator still
// refuses a bare "" for any direct caller (criterion 3 lives on the
// validator seam, pinned in the accepts/refuses tables above).
func TestValidateSudoersUserPlanWiring(t *testing.T) {
	tempRoot(t)
	for _, u := range []string{
		"ALL",
		"NOPASSWD",
		"Cmnd_Alias",
		"mischief\nkara",
		"mischief;id",
		"mischief kara",
	} {
		_, err := PlanInstall(InstallOpts{SudoersUser: u, Prefix: DefaultPrefix})
		if err == nil {
			t.Fatalf("PlanInstall(SudoersUser=%q) planned through an unvalidated grantee", u)
		}
		var ie *InstallError
		if !errors.As(err, &ie) {
			t.Fatalf("PlanInstall(SudoersUser=%q) refused with %T, want InstallError", u, err)
		}
	}
	// the empty contract: "" plans as the DEFAULT grantee, never as an
	// empty line in the drop-in
	p, err := PlanInstall(InstallOpts{SudoersUser: "", Prefix: DefaultPrefix})
	if err != nil {
		t.Fatalf("default-grantee plan refused: %v", err)
	}
	for _, a := range p.Actions {
		if a.Path == DefaultDropInPath && !strings.Contains(string(a.Bytes), defaultSudoersUser+" ALL=(root) NOPASSWD: SETENV: MISCHIEF_CMNDS\n") {
			t.Fatalf("empty --user did not plan the default grantee:\n%s", a.Bytes)
		}
	}
}

// TestValidateSudoersUserPoisonNeverReachesRender (the RED-proven arm):
// the exploit from the board row, driven through the plan, produces NO
// drop-in action carrying a second grant line — and the validator's
// refusal message carries the malicious payload inside %q quoting (the
// diagnostics stay audit-safe). Before the fix, this test failed at the
// first assertion: the plan rendered the 17th-line grant for kara.
func TestValidateSudoersUserPoisonNeverReachesRender(t *testing.T) {
	tempRoot(t)
	poison := "mischief ALL=(root) NOPASSWD: MISCHIEF_CMNDS\nkara ALL=(root) NOPASSWD: ALL #"
	_, err := PlanInstall(InstallOpts{SudoersUser: poison, Prefix: DefaultPrefix})
	if err == nil {
		t.Fatal("the board-row exploit planned through — injection hole is open")
	}
	if body := RenderDropIn(poison); strings.Count(string(body), "NOPASSWD: ALL") != 1 {
		// premise: the raw render really is poisoned (2nd ALL grant line)
		t.Fatalf("premise: expected the exploit payload in the unvalidated render, got:\n%s", body)
	}
}

// TestValidUserInstallsUnchanged: a valid --user still lands in the render
// exactly as before the fix — validation gates input, the template is
// byte-identical.
func TestValidUserInstallsUnchanged(t *testing.T) {
	tempRoot(t)
	p, err := PlanInstall(InstallOpts{SudoersUser: "svc-ops", Prefix: DefaultPrefix})
	if err != nil {
		t.Fatalf("valid --user refused: %v", err)
	}
	var drop *Action
	for i := range p.Actions {
		if p.Actions[i].Path == DefaultDropInPath {
			drop = &p.Actions[i]
		}
	}
	if drop == nil {
		t.Fatal("plan carries no drop-in action")
	}
	want := fmt.Sprintf("%s ALL=(root) NOPASSWD: SETENV: MISCHIEF_CMNDS\n", "svc-ops")
	if !strings.Contains(string(drop.Bytes), want) {
		t.Fatalf("render for a valid user changed:\n%s", drop.Bytes)
	}
	if len(p.Actions) != 3 {
		t.Fatalf("actions: %d, want 3 (mkdir, drop-in, manifest)", len(p.Actions))
	}
}
