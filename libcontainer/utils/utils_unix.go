//go:build !windows
// +build !windows

package utils

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"sync"

	"golang.org/x/sys/unix"
)

// EnsureProcHandle returns whether or not the given file handle is on procfs.
func EnsureProcHandle(fh *os.File) error {
	var buf unix.Statfs_t
	if err := unix.Fstatfs(int(fh.Fd()), &buf); err != nil {
		return fmt.Errorf("ensure %s is on procfs: %w", fh.Name(), err)
	}
	if buf.Type != unix.PROC_SUPER_MAGIC {
		return fmt.Errorf("%s is not on procfs", fh.Name())
	}
	return nil
}

var (
	haveCloseRangeCloexecBool bool
	haveCloseRangeCloexecOnce sync.Once
)

func haveCloseRangeCloexec() bool {
	haveCloseRangeCloexecOnce.Do(func() {
		// Make sure we're not closing a random file descriptor.
		tmpFd, err := unix.FcntlInt(0, unix.F_DUPFD_CLOEXEC, 0)
		if err != nil {
			return
		}
		defer unix.Close(tmpFd)

		err = unix.CloseRange(uint(tmpFd), uint(tmpFd), unix.CLOSE_RANGE_CLOEXEC)
		// Any error means we cannot use close_range(CLOSE_RANGE_CLOEXEC).
		// -ENOSYS and -EINVAL ultimately mean we don't have support, but any
		// other potential error would imply that even the most basic close
		// operation wouldn't work.
		haveCloseRangeCloexecBool = err == nil
	})
	return haveCloseRangeCloexecBool
}

// CloseExecFrom applies O_CLOEXEC to all file descriptors currently open for
// the process (except for those below the given fd value).
func CloseExecFrom(minFd int) error {
	if haveCloseRangeCloexec() {
		err := unix.CloseRange(uint(minFd), math.MaxUint, unix.CLOSE_RANGE_CLOEXEC)
		return os.NewSyscallError("close_range", err)
	}

	fdDir, err := os.Open("/proc/self/fd")
	if err != nil {
		return err
	}
	defer fdDir.Close()

	if err := EnsureProcHandle(fdDir); err != nil {
		return err
	}

	fdList, err := fdDir.Readdirnames(-1)
	if err != nil {
		return err
	}
	for _, fdStr := range fdList {
		fd, err := strconv.Atoi(fdStr)
		// Ignore non-numeric file names.
		if err != nil {
			continue
		}
		// Ignore descriptors lower than our specified minimum.
		if fd < minFd {
			continue
		}
		// Intentionally ignore errors from unix.CloseOnExec -- the cases where
		// this might fail are basically file descriptors that have already
		// been closed (including and especially the one that was created when
		// os.ReadDir did the "opendir" syscall).
		unix.CloseOnExec(fd)
	}
	return nil
}

// NewSockPair returns a new unix socket pair
func NewSockPair(name string) (parent *os.File, child *os.File, err error) {
	fds, err := unix.Socketpair(unix.AF_LOCAL, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return nil, nil, err
	}
	return os.NewFile(uintptr(fds[1]), name+"-p"), os.NewFile(uintptr(fds[0]), name+"-c"), nil
}
