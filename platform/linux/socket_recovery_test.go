//go:build linux

package linux

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestControlSocketCrashRecovery(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires isolated root")
	}
	if os.Getenv("MERIDIAN_SOCKET_CHILD") == "1" {
		dir := os.Getenv("MERIDIAN_SOCKET_DIR")
		lock, err := acquireHelperLockAt(dir)
		if err != nil {
			os.Exit(3)
		}
		defer lock.Close()
		l, err := listenControlAt(dir, 1000)
		if err != nil {
			os.Exit(4)
		}
		defer l.Close()
		fmt.Println("READY")
		select {}
	}
	dir := filepath.Join(t.TempDir(), "trusted")
	c := exec.Command(os.Args[0], "-test.run=^TestControlSocketCrashRecovery$")
	c.Env = append(os.Environ(), "MERIDIAN_SOCKET_CHILD=1", "MERIDIAN_SOCKET_DIR="+dir)
	out, err := c.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Process.Kill(); _ = c.Wait() })
	line, err := bufio.NewReader(out).ReadString('\n')
	if err != nil || line != "READY\n" {
		t.Fatalf("child: %q %v", line, err)
	}
	if err = c.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = c.Wait()
	lock, err := acquireHelperLockAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	l, err := listenControlAt(dir, 1000)
	if err != nil {
		t.Fatalf("verified stale socket not recovered: %v", err)
	}
	l.Close()
}
func TestControlRecoveryRefusesUnknownIdentity(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires isolated root")
	}
	dir := filepath.Join(t.TempDir(), "trusted")
	lock, err := acquireHelperLockAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	l, err := listenControlAt(dir, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if err = recoverControlSocket(dir, 1000); err == nil {
		t.Fatal("active socket removed")
	}
	if err = os.Remove(filepath.Join(dir, "control.identity")); err != nil {
		t.Fatal(err)
	}
	if err = recoverControlSocket(dir, 1000); err == nil {
		t.Fatal("unmarked socket removed")
	}
	if _, err = os.Lstat(filepath.Join(dir, "control.sock")); err != nil {
		t.Fatal("socket missing")
	}
	l.Close()
}

func TestControlRecoveryRejectsChangedIdentity(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires isolated root")
	}
	dir := filepath.Join(t.TempDir(), "trusted")
	lock, err := acquireHelperLockAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	l, err := listenControlAt(dir, 1000)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	marker := filepath.Join(dir, "control.identity")
	if err = os.WriteFile(marker, []byte(`{"device":1,"inode":1,"uid":1000}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err = recoverControlSocket(dir, 1000); err == nil {
		t.Fatal("mismatched identity accepted")
	}
	if _, err = os.Lstat(filepath.Join(dir, "control.sock")); err != nil {
		t.Fatal("socket removed")
	}
}
