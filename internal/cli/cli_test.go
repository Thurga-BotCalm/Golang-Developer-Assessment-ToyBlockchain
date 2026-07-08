package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func run(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = Run(args, &out, &errOut)
	return out.String(), errOut.String(), code
}

func TestEndToEndFlow(t *testing.T) {
	data := filepath.Join(t.TempDir(), "chain.json")

	if out, _, code := run(t, "faucet", "-data", data, "-difficulty", "1", "-to", "Alice", "-amount", "100"); code != 0 || !strings.Contains(out, "funded") {
		t.Fatalf("faucet: code=%d out=%q", code, out)
	}

	if out, _, code := run(t, "mine", "-data", data); code != 0 || !strings.Contains(out, "mined block 1") {
		t.Fatalf("mine (fund): code=%d out=%q", code, out)
	}

	if out, _, code := run(t, "addtx", "-data", data, "-from", "Alice", "-to", "Bob", "-amount", "30"); code != 0 || !strings.Contains(out, "added") {
		t.Fatalf("addtx: code=%d out=%q", code, out)
	}

	if out, _, code := run(t, "mine", "-data", data); code != 0 || !strings.Contains(out, "mined block 2") {
		t.Fatalf("mine (transfer): code=%d out=%q", code, out)
	}

	if out, _, code := run(t, "balances", "-data", data); code != 0 || !strings.Contains(out, "Alice: 70") || !strings.Contains(out, "Bob: 30") {
		t.Fatalf("balances: code=%d out=%q", code, out)
	}

	if out, _, code := run(t, "validate", "-data", data); code != 0 || !strings.Contains(out, "VALID") {
		t.Fatalf("validate: code=%d out=%q", code, out)
	}

	if out, _, code := run(t, "print", "-data", data); code != 0 || !strings.Contains(out, "Block 2") {
		t.Fatalf("print: code=%d out=%q", code, out)
	}
}

func TestOverspendIsRejectedViaCLI(t *testing.T) {
	data := filepath.Join(t.TempDir(), "chain.json")

	if _, _, code := run(t, "faucet", "-data", data, "-difficulty", "1", "-to", "Alice", "-amount", "100"); code != 0 {
		t.Fatalf("faucet setup failed")
	}
	if _, _, code := run(t, "mine", "-data", data); code != 0 {
		t.Fatalf("mine setup failed")
	}

	_, errOut, code := run(t, "addtx", "-data", data, "-from", "Alice", "-to", "Bob", "-amount", "150")
	if code == 0 {
		t.Fatal("expected non-zero exit code for rejected overspend")
	}
	if !strings.Contains(errOut, "rejected") {
		t.Fatalf("stderr = %q, want it to mention rejection", errOut)
	}

	out, _, _ := run(t, "balances", "-data", data)
	if !strings.Contains(out, "Alice: 100") {
		t.Fatalf("balances after rejected tx = %q, want Alice: 100", out)
	}
}

func TestUnknownCommand(t *testing.T) {
	_, errOut, code := run(t, "bogus")
	if code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
	if !strings.Contains(errOut, "unknown command") {
		t.Fatalf("stderr = %q, want mention of unknown command", errOut)
	}
}
