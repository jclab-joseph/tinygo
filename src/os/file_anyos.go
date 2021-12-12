// +build !baremetal,!js

// Portions copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package os

import (
	"io"
	"syscall"
)

func init() {
	// Mount the host filesystem at the root directory. This is what most
	// programs will be expecting.
	Mount("/", unixFilesystem{})
}

// Stdin, Stdout, and Stderr are open Files pointing to the standard input,
// standard output, and standard error file descriptors.
var (
	Stdin  = &File{unixFileHandle(syscall.Stdin), "/dev/stdin"}
	Stdout = &File{unixFileHandle(syscall.Stdout), "/dev/stdout"}
	Stderr = &File{unixFileHandle(syscall.Stderr), "/dev/stderr"}
)

const DevNull = "/dev/null"

// isOS indicates whether we're running on a real operating system with
// filesystem support.
const isOS = true

// Chdir changes the current working directory to the named directory.
// If there is an error, it will be of type *PathError.
func Chdir(dir string) error {
	if e := syscall.Chdir(dir); e != nil {
		return &PathError{Op: "chdir", Path: dir, Err: e}
	}
	return nil
}

// unixFilesystem is an empty handle for a Unix/Linux filesystem. All operations
// are relative to the current working directory.
type unixFilesystem struct {
}

func (fs unixFilesystem) Mkdir(path string, perm FileMode) error {
	return handleSyscallError(syscall.Mkdir(path, uint32(perm)))
}

func (fs unixFilesystem) Remove(path string) error {
	// System call interface forces us to know
	// whether name is a file or directory.
	// Try both: it is cheaper on average than
	// doing a Stat plus the right one.
	e := handleSyscallError(syscall.Unlink(path))
	if e == nil {
		return nil
	}
	e1 := handleSyscallError(syscall.Rmdir(path))
	if e1 == nil {
		return nil
	}

	// Both failed: figure out which error to return.
	// OS X and Linux differ on whether unlink(dir)
	// returns EISDIR, so can't use that. However,
	// both agree that rmdir(file) returns ENOTDIR,
	// so we can use that to decide which error is real.
	// Rmdir might also return ENOTDIR if given a bad
	// file path, like /etc/passwd/foo, but in that case,
	// both errors will be ENOTDIR, so it's okay to
	// use the error from unlink.
	if e1 != syscall.ENOTDIR {
		e = e1
	}
	return &PathError{Op: "remove", Path: path, Err: e}
}

func (fs unixFilesystem) OpenFile(path string, flag int, perm FileMode) (FileHandle, error) {
	// Map os package flags to syscall flags.
	syscallFlag := 0
	if flag&O_RDONLY != 0 {
		syscallFlag |= syscall.O_RDONLY
	}
	if flag&O_WRONLY != 0 {
		syscallFlag |= syscall.O_WRONLY
	}
	if flag&O_RDWR != 0 {
		syscallFlag |= syscall.O_RDWR
	}
	if flag&O_APPEND != 0 {
		syscallFlag |= syscall.O_APPEND
	}
	if flag&O_CREATE != 0 {
		syscallFlag |= syscall.O_CREAT
	}
	if flag&O_EXCL != 0 {
		syscallFlag |= syscall.O_EXCL
	}
	if flag&O_SYNC != 0 {
		syscallFlag |= syscall.O_SYNC
	}
	if flag&O_TRUNC != 0 {
		syscallFlag |= syscall.O_TRUNC
	}
	fp, err := syscall.Open(path, syscallFlag, uint32(perm))
	return unixFileHandle(fp), handleSyscallError(err)
}

// unixFileHandle is a Unix file pointer with associated methods that implement
// the FileHandle interface.
type unixFileHandle uintptr

// Read reads up to len(b) bytes from the File. It returns the number of bytes
// read and any error encountered. At end of file, Read returns 0, io.EOF.
func (f unixFileHandle) Read(b []byte) (n int, err error) {
	n, err = syscall.Read(syscallFd(f), b)
	err = handleSyscallError(err)
	if n == 0 && err == nil {
		err = io.EOF
	}
	return
}

// Write writes len(b) bytes to the File. It returns the number of bytes written
// and an error, if any. Write returns a non-nil error when n != len(b).
func (f unixFileHandle) Write(b []byte) (n int, err error) {
	n, err = syscall.Write(syscallFd(f), b)
	err = handleSyscallError(err)
	return
}

// Close closes the File, rendering it unusable for I/O.
func (f unixFileHandle) Close() error {
	return handleSyscallError(syscall.Close(syscallFd(f)))
}

// Chmod changes the mode of the named file to mode.
// If the file is a symbolic link, it changes the mode of the link's target.
// If there is an error, it will be of type *PathError.
//
// A different subset of the mode bits are used, depending on the
// operating system.
//
// On Unix, the mode's permission bits, ModeSetuid, ModeSetgid, and
// ModeSticky are used.
//
// On Windows, only the 0200 bit (owner writable) of mode is used; it
// controls whether the file's read-only attribute is set or cleared.
// The other bits are currently unused. For compatibility with Go 1.12
// and earlier, use a non-zero mode. Use mode 0400 for a read-only
// file and 0600 for a readable+writable file.
func Chmod(name string, mode FileMode) error {
	longName := fixLongPath(name)
	e := ignoringEINTR(func() error {
		return syscall.Chmod(longName, syscallMode(mode))
	})
	if e != nil {
		return &PathError{Op: "chmod", Path: name, Err: e}
	}
	return nil
}

// ignoringEINTR makes a function call and repeats it if it returns an
// EINTR error. This appears to be required even though we install all
// signal handlers with SA_RESTART: see #22838, #38033, #38836, #40846.
// Also #20400 and #36644 are issues in which a signal handler is
// installed without setting SA_RESTART. None of these are the common case,
// but there are enough of them that it seems that we can't avoid
// an EINTR loop.
func ignoringEINTR(fn func() error) error {
	for {
		err := fn()
		if err != syscall.EINTR {
			return err
		}
	}
}

// handleSyscallError converts syscall errors into regular os package errors.
// The err parameter must be either nil or of type syscall.Errno.
func handleSyscallError(err error) error {
	if err == nil {
		return nil
	}
	switch err.(syscall.Errno) {
	case syscall.EEXIST:
		return ErrExist
	case syscall.ENOENT:
		return ErrNotExist
	default:
		return err
	}
}

// syscallMode returns the syscall-specific mode bits from Go's portable mode bits.
func syscallMode(i FileMode) (o uint32) {
	o |= uint32(i.Perm())
	if i&ModeSetuid != 0 {
		o |= syscall.S_ISUID
	}
	if i&ModeSetgid != 0 {
		o |= syscall.S_ISGID
	}
	if i&ModeSticky != 0 {
		o |= syscall.S_ISVTX
	}
	// No mapping for Go's ModeTemporary (plan9 only).
	return
}