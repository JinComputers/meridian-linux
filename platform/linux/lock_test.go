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

func TestHelperProcessLock(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires isolated root")
	}
	if os.Getenv("MERIDIAN_LOCK_CHILD") == "1" {
		f, err := acquireHelperLockAt(os.Getenv("MERIDIAN_LOCK_DIR"))
		if err != nil {
			os.Exit(3)
		}
		f.Close()
		os.Exit(0)
	}
	dir := filepath.Join(t.TempDir(), "trusted")
	first, err := acquireHelperLockAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	child := func() error {
		c := exec.Command(os.Args[0], "-test.run=^TestHelperProcessLock$")
		c.Env = append(os.Environ(), "MERIDIAN_LOCK_CHILD=1", "MERIDIAN_LOCK_DIR="+dir)
		return c.Run()
	}
	if err := child(); err == nil {
		t.Fatal("another process acquired a held lock")
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if err := child(); err != nil {
		t.Fatalf("lock not released on close: %v", err)
	}
}
func TestHelperLockRejectsSymlinkAndWritableDirectory(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires isolated root")
	}
	dir := filepath.Join(t.TempDir(), "trusted")
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/dev/null", filepath.Join(dir, "helper.lock")); err != nil {
		t.Fatal(err)
	}
	if f, err := acquireHelperLockAt(dir); err == nil {
		f.Close()
		t.Fatal("symlink accepted")
	}
	if err := os.Remove(filepath.Join(dir, "helper.lock")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0777); err != nil {
		t.Fatal(err)
	}
	if f, err := acquireHelperLockAt(dir); err == nil {
		f.Close()
		t.Fatal("writable directory accepted")
	}
}

func TestHelperLockReleasedWhenOwnerDies(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires isolated root")
	}
	if os.Getenv("MERIDIAN_LOCK_HOLDER") == "1" {
		f, err := acquireHelperLockAt(os.Getenv("MERIDIAN_LOCK_DIR"))
		if err != nil {
			os.Exit(3)
		}
		fmt.Println("LOCKED")
		defer f.Close()
		select {}
	}
	dir := filepath.Join(t.TempDir(), "trusted")
	c := exec.Command(os.Args[0], "-test.run=^TestHelperLockReleasedWhenOwnerDies$")
	c.Env = append(os.Environ(), "MERIDIAN_LOCK_HOLDER=1", "MERIDIAN_LOCK_DIR="+dir)
	out, err := c.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Process.Kill(); _ = c.Wait() })
	line, err := bufio.NewReader(out).ReadString('\n')
	if err != nil || line != "LOCKED\n" {
		t.Fatalf("child failed to hold lock: %q %v", line, err)
	}
	if f, err := acquireHelperLockAt(dir); err == nil {
		f.Close()
		t.Fatal("child lock ignored")
	}
	if err = c.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = c.Wait()
	f, err := acquireHelperLockAt(dir)
	if err != nil {
		t.Fatalf("lock remained after death: %v", err)
	}
	f.Close()
}
