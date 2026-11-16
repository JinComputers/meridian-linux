//go:build linux

package linux

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

const ControlSocketPath = "/run/meridian/control.sock"

// PeerUID reads immutable kernel credentials of the connected peer.
func PeerUID(c net.Conn) (uint32, error) {
	uc, ok := c.(*net.UnixConn)
	if !ok {
		return 0, errors.New("not a Unix connection")
	}
	raw, err := uc.SyscallConn()
	if err != nil {
		return 0, err
	}
	var cred *unix.Ucred
	var socketErr error
	err = raw.Control(func(fd uintptr) { cred, socketErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED) })
	if err != nil {
		return 0, err
	}
	if socketErr != nil {
		return 0, socketErr
	}
	if cred == nil {
		return 0, errors.New("no peer credentials")
	}
	return cred.Uid, nil
}

// ListenControl requires a root-owned non-writable directory and never unlinks
// a pre-existing socket, even one that appears stale. A root administrator must
// clean it after inspecting ownership if the previous service died uncleanly.
func ListenControl(uid uint32) (net.Listener, error) { return listenControlAt("/run/meridian", uid) }

func listenControlAt(dir string, uid uint32) (net.Listener, error) {
	if os.Geteuid() != 0 || uid == 0 || uid == ^uint32(0) {
		return nil, errors.New("requires root and an explicitly selected non-root UID")
	}
	path := filepath.Join(dir, "control.sock")
	if err := os.Mkdir(dir, 0755); err != nil && !os.IsExist(err) {
		return nil, err
	}
	fi, err := os.Lstat(dir)
	if err != nil {
		return nil, err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok || !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 || st.Uid != 0 || fi.Mode().Perm()&0022 != 0 {
		return nil, errors.New("untrusted control directory")
	}
	if _, err = os.Lstat(path); err == nil {
		return nil, errors.New("control path already exists; refusing to remove it")
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	l, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	cleanup := func(err error) (net.Listener, error) { l.Close(); return nil, err }
	if err = os.Chmod(path, 0600); err != nil {
		return cleanup(err)
	}
	if err = os.Chown(path, int(uid), 0); err != nil {
		return cleanup(err)
	}
	return l, nil
}
func VerifyPeer(c net.Conn, expectedUID uint32) error {
	uid, err := PeerUID(c)
	if err != nil {
		return err
	}
	if uid != expectedUID {
		return fmt.Errorf("unauthorized peer uid %d", uid)
	}
	return nil
}
