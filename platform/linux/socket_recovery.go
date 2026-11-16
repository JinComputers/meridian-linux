//go:build linux

package linux

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"
)

type socketIdentity struct {
	Device uint64 `json:"device"`
	Inode  uint64 `json:"inode"`
	UID    uint32 `json:"uid"`
}

// Called only while the helper's exclusive process lock is held. A legacy
// socket without an identity file is never removed automatically.
func recoverControlSocket(dir string, uid uint32) error {
	path := filepath.Join(dir, "control.sock")
	marker := filepath.Join(dir, "control.identity")
	_, socketErr := os.Lstat(path)
	if socketErr != nil && !os.IsNotExist(socketErr) {
		return socketErr
	}
	id, err := readSocketIdentity(marker)
	if os.IsNotExist(err) {
		if os.IsNotExist(socketErr) {
			return nil
		}
		return errors.New("control socket has no trusted identity")
	}
	if err != nil {
		return err
	}
	if id.UID != uid {
		return errors.New("control socket identity UID mismatch")
	}
	if os.IsNotExist(socketErr) {
		return os.Remove(marker)
	}
	var st unix.Stat_t
	if err := unix.Lstat(path, &st); err != nil {
		return err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFSOCK || st.Uid != uid || st.Gid != 0 || st.Mode&0777 != 0600 || st.Nlink != 1 || uint64(st.Dev) != id.Device || st.Ino != id.Inode {
		return errors.New("control socket identity mismatch")
	}
	c, err := net.DialTimeout("unix", path, 500*time.Millisecond)
	if err == nil {
		c.Close()
		return errors.New("control socket is active")
	}
	if !errors.Is(err, unix.ECONNREFUSED) {
		return fmt.Errorf("control socket status unknown: %w", err)
	}
	var again unix.Stat_t
	if err = unix.Lstat(path, &again); err != nil {
		return err
	}
	if again.Dev != st.Dev || again.Ino != st.Ino || again.Mode != st.Mode || again.Uid != st.Uid {
		return errors.New("control socket changed during recovery")
	}
	if err = os.Remove(path); err != nil {
		return err
	}
	return os.Remove(marker)
}
func readSocketIdentity(path string) (socketIdentity, error) {
	var id socketIdentity
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return id, err
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	var st unix.Stat_t
	if err = unix.Fstat(fd, &st); err != nil {
		return id, err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG || st.Uid != 0 || st.Mode&0077 != 0 || st.Nlink != 1 || st.Size > 512 {
		return id, errors.New("untrusted control identity")
	}
	b, err := io.ReadAll(io.LimitReader(f, 513))
	if err != nil {
		return id, err
	}
	if len(b) > 512 {
		return id, errors.New("invalid control identity")
	}
	if err = json.Unmarshal(b, &id); err != nil {
		return id, err
	}
	if id.Inode == 0 {
		return id, errors.New("invalid control identity")
	}
	return id, nil
}
func saveSocketIdentity(dir string, uid uint32) error {
	path := filepath.Join(dir, "control.sock")
	marker := filepath.Join(dir, "control.identity")
	var st unix.Stat_t
	if err := unix.Lstat(path, &st); err != nil {
		return err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFSOCK || st.Uid != uid || st.Gid != 0 || st.Mode&0777 != 0600 || st.Nlink != 1 {
		return errors.New("unexpected newly created control socket")
	}
	b, err := json.Marshal(socketIdentity{Device: uint64(st.Dev), Inode: st.Ino, UID: uid})
	if err != nil {
		return err
	}
	fd, err := unix.Open(marker, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return err
	}
	f := os.NewFile(uintptr(fd), marker)
	_, err = f.Write(b)
	if err == nil {
		err = f.Sync()
	}
	cerr := f.Close()
	if err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(marker)
	}
	return err
}
