//go:build linux

package linux

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/JinComputers/meridian-desktop/core"
	"golang.org/x/sys/unix"
)

// RecoverJournal requires the caller to hold AcquireHelperLock throughout.
// It is an explicit administrative operation: successful recovery removes the
// fail-closed firewall and restores direct network access, without reconnecting.
func RecoverJournal(ctx context.Context, platform *Platform) error {
	return recoverJournalAt(ctx, "/run/meridian", platform)
}

func recoverJournalAt(ctx context.Context, dir string, platform *Platform) error {
	if os.Geteuid() != 0 {
		return errors.New("recovery requires root")
	}
	d, err := unix.Open(dir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer unix.Close(d)
	var ds unix.Stat_t
	if err = unix.Fstat(d, &ds); err != nil {
		return err
	}
	if ds.Uid != 0 || ds.Mode&0022 != 0 {
		return errors.New("untrusted recovery directory")
	}
	fd, err := unix.Openat(d, "state.json", unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if errors.Is(err, unix.ENOENT) {
		return nil
	}
	if err != nil {
		return err
	}
	f := os.NewFile(uintptr(fd), "state.json")
	defer f.Close()
	var st unix.Stat_t
	if err = unix.Fstat(fd, &st); err != nil {
		return err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG || st.Uid != 0 || st.Gid != 0 || st.Nlink != 1 || st.Mode&0077 != 0 || st.Size > 65536 {
		return errors.New("untrusted network journal")
	}
	data, err := io.ReadAll(io.LimitReader(f, 65537))
	if err != nil {
		return err
	}
	if len(data) > 65536 {
		return errors.New("oversize network journal")
	}
	var state core.State
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err = dec.Decode(&state); err != nil {
		return fmt.Errorf("invalid network journal: %w", err)
	}
	var extra any
	if err = dec.Decode(&extra); err != io.EOF {
		return errors.New("invalid trailing journal data")
	}
	if state.PID <= 0 || state.StartedAt.IsZero() || !validInterfaceName(state.PhysicalInterface) || (state.UtunName != "" && !validInterfaceName(state.UtunName)) || ((state.FullTunnelSet || state.TunnelDNSSet) && state.UtunName == "") || (state.IPv6Off && !validFirewallName(state.IPv6Service)) || (!state.IPv6Off && state.IPv6Service != "") || state.PrimaryService != "" || len(state.OriginalDNS) > 0 || len(state.ExceptionTargets) > 0 || state.DirectDNSSet || len(state.RUDirectRoutes) > 0 {
		return errors.New("journal does not describe a supported Linux session")
	}
	if platform.ProcessAlive(state.PID) {
		return fmt.Errorf("journal belongs to a live process (%d)", state.PID)
	}
	if err = platform.Restore(ctx, &state); err != nil {
		return fmt.Errorf("network recovery incomplete; journal retained: %w", err)
	}
	var current unix.Stat_t
	if err = unix.Fstatat(d, "state.json", &current, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return err
	}
	if current.Dev != st.Dev || current.Ino != st.Ino || current.Mode != st.Mode || current.Uid != st.Uid || current.Nlink != st.Nlink {
		return errors.New("network journal changed during recovery")
	}
	if err = unix.Unlinkat(d, "state.json", 0); err != nil {
		return err
	}
	if err = unix.Fsync(d); err != nil {
		return fmt.Errorf("journal removed but directory sync failed: %w", err)
	}
	return nil
}
