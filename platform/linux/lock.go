//go:build linux

package linux

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// AcquireHelperLock holds an advisory lock until the returned file is closed.
// Never unlink the lock file: replacing its inode permits two lock owners.
func AcquireHelperLock() (*os.File, error) { return acquireHelperLockAt("/run/meridian") }

func acquireHelperLockAt(dir string) (*os.File, error) {
	if os.Geteuid() != 0 {
		return nil, errors.New("helper lock requires root")
	}
	if err := os.Mkdir(dir, 0755); err != nil && !os.IsExist(err) {
		return nil, err
	}
	d, err := unix.Open(dir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	defer unix.Close(d)
	var ds unix.Stat_t
	if err = unix.Fstat(d, &ds); err != nil {
		return nil, err
	}
	if ds.Uid != 0 || ds.Mode&0022 != 0 {
		return nil, errors.New("untrusted helper directory")
	}
	fd, err := unix.Openat(d, "helper.lock", unix.O_RDWR|unix.O_CREAT|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return nil, err
	}
	var st unix.Stat_t
	if err = unix.Fstat(fd, &st); err != nil {
		unix.Close(fd)
		return nil, err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG || st.Uid != 0 || st.Nlink != 1 || st.Mode&0077 != 0 {
		unix.Close(fd)
		return nil, errors.New("untrusted helper lock file")
	}
	if err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("helper already running or lock unavailable: %w", err)
	}
	return os.NewFile(uintptr(fd), "helper.lock"), nil
}
