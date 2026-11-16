//go:build linux

package linux

import (
	"net"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestPeerUIDAndRejection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "peer.sock")
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	done := make(chan error, 1)
	go func() {
		c, e := net.Dial("unix", path)
		if e != nil {
			done <- e
			return
		}
		defer c.Close()
		_, e = c.Write([]byte{1})
		done <- e
	}()
	c, err := l.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var b [1]byte
	if _, err = c.Read(b[:]); err != nil {
		t.Fatal(err)
	}
	uid, err := PeerUID(c)
	if err != nil {
		t.Fatal(err)
	}
	if uid != uint32(os.Getuid()) {
		t.Fatalf("got UID %d", uid)
	}
	if err = VerifyPeer(c, uid); err != nil {
		t.Fatal(err)
	}
	if err = VerifyPeer(c, uid+1); err == nil {
		t.Fatal("accepted wrong UID")
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
}

func TestListenOwnershipAndExistingPath(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root")
	}
	dir := t.TempDir()
	const uid = 65534
	l, err := listenControlAt(dir, uid)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "control.sock")
	fi, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	st := fi.Sys().(*syscall.Stat_t)
	if st.Uid != uid || fi.Mode().Perm() != 0600 {
		t.Fatalf("unsafe socket owner=%d mode=%v", st.Uid, fi.Mode())
	}
	if _, err = listenControlAt(dir, uid); err == nil {
		t.Fatal("accepted existing socket")
	}
	if err = l.Close(); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = listenControlAt(dir, uid); err == nil {
		t.Fatal("accepted existing file")
	}
	if b, err := os.ReadFile(path); err != nil || string(b) != "keep" {
		t.Fatal("existing file altered")
	}
	if _, err = listenControlAt(dir, 0); err == nil {
		t.Fatal("accepted root UID")
	}
	if _, err = listenControlAt(dir, ^uint32(0)); err == nil {
		t.Fatal("accepted sentinel UID")
	}
}
func TestRejectWritableDirectory(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0777); err != nil {
		t.Fatal(err)
	}
	if _, err := listenControlAt(dir, 65534); err == nil {
		t.Fatal("accepted writable directory")
	}
}
