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

// genKey runs keygen against a fresh key file in the test's temp dir and
// returns its path, ready to pass to addtx via -key.
func genKey(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name+".key")
	if out, _, code := run(t, "keygen", "-out", path); code != 0 || !strings.Contains(out, "address:") {
		t.Fatalf("keygen: code=%d out=%q", code, out)
	}
	return path
}

func TestEndToEndFlow(t *testing.T) {
	data := filepath.Join(t.TempDir(), "chain.json")
	aliceKey := genKey(t, "alice")

	if out, _, code := run(t, "faucet", "-data", data, "-difficulty", "1", "-to", "Alice", "-amount", "100"); code != 0 || !strings.Contains(out, "funded") {
		t.Fatalf("faucet: code=%d out=%q", code, out)
	}

	if out, _, code := run(t, "mine", "-data", data); code != 0 || !strings.Contains(out, "mined block 1") {
		t.Fatalf("mine (fund): code=%d out=%q", code, out)
	}

	if out, _, code := run(t, "addtx", "-data", data, "-key", aliceKey, "-from", "Alice", "-to", "Bob", "-amount", "30"); code != 0 || !strings.Contains(out, "added") {
		t.Fatalf("addtx: code=%d out=%q stderr=%q", code, out, out)
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
	aliceKey := genKey(t, "alice")

	if _, _, code := run(t, "faucet", "-data", data, "-difficulty", "1", "-to", "Alice", "-amount", "100"); code != 0 {
		t.Fatalf("faucet setup failed")
	}
	if _, _, code := run(t, "mine", "-data", data); code != 0 {
		t.Fatalf("mine setup failed")
	}

	_, errOut, code := run(t, "addtx", "-data", data, "-key", aliceKey, "-from", "Alice", "-to", "Bob", "-amount", "150")
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

func TestAddTxWithoutKeyIsRejected(t *testing.T) {
	data := filepath.Join(t.TempDir(), "chain.json")

	if _, _, code := run(t, "faucet", "-data", data, "-difficulty", "1", "-to", "Alice", "-amount", "100"); code != 0 {
		t.Fatalf("faucet setup failed")
	}
	if _, _, code := run(t, "mine", "-data", data); code != 0 {
		t.Fatalf("mine setup failed")
	}

	_, errOut, code := run(t, "addtx", "-data", data, "-from", "Alice", "-to", "Bob", "-amount", "10")
	if code == 0 {
		t.Fatal("expected non-zero exit code when -key is missing")
	}
	if !strings.Contains(errOut, "-key") {
		t.Fatalf("stderr = %q, want it to mention the missing -key flag", errOut)
	}
}

func TestImpersonationIsRejectedViaCLI(t *testing.T) {
	data := filepath.Join(t.TempDir(), "chain.json")
	aliceKey := genKey(t, "alice")
	eveKey := genKey(t, "eve")

	if _, _, code := run(t, "faucet", "-data", data, "-difficulty", "1", "-to", "Alice", "-amount", "100"); code != 0 {
		t.Fatalf("faucet setup failed")
	}
	if _, _, code := run(t, "mine", "-data", data); code != 0 {
		t.Fatalf("mine setup failed")
	}
	if _, _, code := run(t, "addtx", "-data", data, "-key", aliceKey, "-from", "Alice", "-to", "Bob", "-amount", "10"); code != 0 {
		t.Fatalf("addtx setup failed")
	}
	if _, _, code := run(t, "mine", "-data", data); code != 0 {
		t.Fatalf("mine setup failed")
	}

	// Eve's key claiming to spend as Alice, now that Alice's key is bound.
	_, errOut, code := run(t, "addtx", "-data", data, "-key", eveKey, "-from", "Alice", "-to", "Eve", "-amount", "5")
	if code == 0 {
		t.Fatal("expected non-zero exit code for an impersonation attempt")
	}
	if !strings.Contains(errOut, "different key") {
		t.Fatalf("stderr = %q, want it to mention the key mismatch", errOut)
	}
}

func TestResolveKeepsLongerChain(t *testing.T) {
	data := filepath.Join(t.TempDir(), "chain.json")
	other := filepath.Join(t.TempDir(), "other.json")

	// Same starting difficulty so both share an identical genesis block.
	if _, _, code := run(t, "faucet", "-data", data, "-difficulty", "1", "-to", "Alice", "-amount", "100"); code != 0 {
		t.Fatalf("faucet setup (data) failed")
	}
	if _, _, code := run(t, "mine", "-data", data); code != 0 {
		t.Fatalf("mine setup (data) failed")
	}
	if _, _, code := run(t, "faucet", "-data", other, "-difficulty", "1", "-to", "Alice", "-amount", "100"); code != 0 {
		t.Fatalf("faucet setup (other) failed")
	}
	if _, _, code := run(t, "mine", "-data", other); code != 0 {
		t.Fatalf("mine setup (other) failed")
	}
	if _, _, code := run(t, "faucet", "-data", other, "-to", "Bob", "-amount", "10"); code != 0 {
		t.Fatalf("faucet extra (other) failed")
	}
	if _, _, code := run(t, "mine", "-data", other); code != 0 {
		t.Fatalf("mine extra (other) failed")
	}

	out, _, code := run(t, "resolve", "-data", data, "-other", other)
	if code != 0 || !strings.Contains(out, "replaced") {
		t.Fatalf("resolve: code=%d out=%q", code, out)
	}

	if out, _, code := run(t, "print", "-data", data); code != 0 || !strings.Contains(out, "Block 2") {
		t.Fatalf("print after resolve: code=%d out=%q", code, out)
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
