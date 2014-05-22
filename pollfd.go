// +build darwin dragonfly freebsd linux netbsd openbsd

// pollfd implements a file-descriptor type (FD) that can be used with
// Go runtime's netpoll mechanism. A "pollfd.FD" is associated with a
// system file-descriptor and can be used to read-from and write-to it
// without forcing the runtime to create an OS-level thread for every
// blocked operation. Blocked operations can, instead, be multiplexed
// by the runtime on the same OS thread using the netpoller mechanism
// (which is typically used for network connections). In addition, the
// package provides support for timeouts on Read and Write operations.
//
// pollfd is especially usefull for interfacing with terminals,
// character devices, FIFOs, named pipes, etc.
//
// Typical usage:
//
//   fd, err: = pollfd.Open("/dev/ttyXX", pollfd.O_RW)
//   if err != nil {
//       log.Fatalln("Failed to open device:", err)
//   }
//
//   ...
//
//   err = fd.SetReadDeadline(time.Now().Add(5 * time.Second))
//   if err != nil {
//       log.Fatalln("Failed to set R deadline:", err)
//   }
//
//   ...
//
//   b := make([]byte, 10)
//   n, err := fd.Read(b)
//   if err != nil {
//       log.Fatalln("Write failed:", err)
//   }
//
//   ...
//
//   n, err = fd.Write([]byte("Test"))
//   if err != nil {
//       log.Fatalln("Write failed:", err)
//   }
//
// Also, pollfd operations are thread-safe; the same FD can be used
// from multiple go-routines. It is, for example, safe to close a file
// descriptor blocked on a Read or Write call from another go-routine.
//
// **ATTENTION:** In order to build and use the pollfd package, you
// must have a tweaked (non-standard) verion of the Go standard
// library, that exports an interface to the netpoller mechanism. You
// can download this from:
//
//       https://code.google.com/r/nickpatavalis-pollfd/
//
// but unless you really, really need it, or you just want to
// toy-around with it... DON'T! (Because a non-standard standard
// library is an oxymoron, and very bad for your karma)
//
package pollfd

import (
	"fmt"
	"io"
	"runtime"
	"runtime/netpoll"
	"syscall"
	"time"
)

// Flags to Open
const (
	O_RW = (syscall.O_NOCTTY |
		syscall.O_CLOEXEC |
		syscall.O_RDWR |
		syscall.O_NONBLOCK) // Open file for read and write
	O_RO = (syscall.O_NOCTTY |
		syscall.O_CLOEXEC |
		syscall.O_RDONLY |
		syscall.O_NONBLOCK) // Open file for read
	O_WO = (syscall.O_NOCTTY |
		syscall.O_CLOEXEC |
		syscall.O_WRONLY |
		syscall.O_NONBLOCK) // Open file for write
	o_MODE = 0666
)

type temporary interface {
	Temporary() bool
}

type timeout interface {
	Timeout() bool
}

// IsErrorTemporary is a helper fuction that checks if the argument
// indicates a termporary error condition.
func IsErrorTemporary(e error) bool {
	t, ok := e.(temporary)
	return ok && t.Temporary()
}

// IsErrorTimeout is a helper fuction that checks if the argument
// indicates a timeout error condition.
func IsErrorTimeout(e error) bool {
	t, ok := e.(timeout)
	return ok && t.Timeout()
}

// Errors returned by pollfd functions and methods. In addition to
// these, pollfd functions and methods may return the errors reported
// by the underlying system calls (open(2), read(2), write(2), etc.),
// as well as io.EOF and io.ErrUnexpectedEOF.
var (
	ErrTimeout error = netpoll.ErrTimeout // Operation timed-out
	ErrClosing error = netpoll.ErrClosing // Operation on closed FD
)

// FD is a file descriptor that can be used with the Go runtime's
// netpoller subsystem. Typically it is a file-descriptor connected to
// a terminal, a pseudo terminal, a character device, a FIFO (named
// pipe), etc.
type FD struct {
	fdmu  netpoll.FdMutex
	sysfd int
	name  string
	pd    netpoll.PollDesc
}

func newFD(sysfd int, name string) (*FD, error) {
	fd := &FD{sysfd: sysfd, name: name}
	if err := fd.pd.Init(uintptr(fd.sysfd)); err != nil {
		return nil, err
	}
	runtime.SetFinalizer(fd, (*FD).Close)
	return fd, nil
}

// Open the named path for reading, writing or both, depnding on the
// flags argument.
func Open(name string, flags int) (*FD, error) {
	sysfd, err := syscall.Open(name, flags, o_MODE)
	if err != nil {
		return nil, err
	}
	return newFD(sysfd, name)
}

// FromSysfd creates, initializes, and returns a pollfd FD from the
// given system file-descriptor. It is the caller's responsibility to
// set "sysfd" to Non-Blocking mode before calling FromSysfd. After
// calling FromSysfd the system file-descriptor must subsequently be
// used only through the FD methods, not directly. The "name" argument
// is used to annotate the FD with a path; if not available it is ok
// to pass nil.
func FromSysfd(sysfd int, name string) (*FD, error) {
	return newFD(sysfd, name)
}

// String returns a text representation of the FD structure formated
// like this: {name sysfd}. Used when printing with %v or with
// fmt.Print/Println.
func (fd *FD) String() string {
	return fmt.Sprintf("{\"%s\" %d}", fd.name, fd.sysfd)
}

// Name returns the path associated with the FD.
func (fd *FD) Name() string {
	return fd.name
}

// Sysfd returns the system file descriptor (Unix fd) encapsulated in
// the FD structure. Usefull for doing misc. operations on the FD
// (e.g. ioctl's and stuff)---but see also the discussion on locking
// (methods: (*FD).Incref, (*FD).Decref)
func (fd *FD) Sysfd() int {
	return fd.sysfd
}

// Incref must be called to "lock" the FD structure before performing
// misc. operations (e.g. ioctl(), setsockopt(), fcntl(), etc.) on the
// underlying system file descriptor (see: (*FD).Sysfd). The typical
// usage pattern is:
//
//    if err := fd.Incref(); err != nil {
//        return err
//    }
//    defer fd.Decref()
//    ... do misc operations on fd.Sysfd() ...
//
// By calling Incref the the file descriptor is protected from
// concurent Close calls issued by other go-routines.
func (fd *FD) Incref() error {
	if !fd.fdmu.Incref() {
		return ErrClosing
	}
	return nil
}

// Decref "unlocks" the FD structure after perfoeming misc. operations
// on the underlying system file descriptor. See (*FD).Inref for more.
func (fd *FD) Decref() {
	if fd.fdmu.Decref() {
		fd.destroy()
	}
}

func (fd *FD) readLock() error {
	if !fd.fdmu.RWLock(true) {
		return ErrClosing
	}
	return nil
}

func (fd *FD) readUnlock() {
	if fd.fdmu.RWUnlock(true) {
		fd.destroy()
	}
}

func (fd *FD) writeLock() error {
	if !fd.fdmu.RWLock(false) {
		return ErrClosing
	}
	return nil
}

func (fd *FD) writeUnlock() {
	if fd.fdmu.RWUnlock(false) {
		fd.destroy()
	}
}

func (fd *FD) destroy() {
	fd.pd.Close()
	syscall.Close(fd.sysfd)
	fd.sysfd = -1
	runtime.SetFinalizer(fd, nil)
}

// Close closes the file descriptor.
func (fd *FD) Close() error {
	// TODO(npat): Is this really needed? Currently fd.pd.Lock()
	// as well as fd.pd.Unlock() and fd.pd.Wakeup() are no-ops.
	fd.pd.Lock()
	if !fd.fdmu.IncrefAndClose() {
		fd.pd.Unlock()
		return ErrClosing
	}
	// Unblock any I/O.  Once it all unblocks and returns,
	// so that it cannot be referring to fd.sysfd anymore,
	// the final decref will close fd.sysfd.
	doWakeup := fd.pd.Evict()
	fd.pd.Unlock()
	fd.Decref()
	if doWakeup {
		fd.pd.Wakeup()
	}
	return nil
}

// Read reads up to len(p) bytes into p.  It returns the number of
// bytes read (0 <= n <= len(p)) and any error encountered. If some
// data is available but not len(p) bytes, Read returns what is
// available instead of waiting for more. Read is formally and
// semantically compatible with the Read method of the io.Reader
// interface. See documentation of this interface method for more
// details. In addition Read honors the timeout set by
// (*FD).SetDeadline and (*FD).SetReadDeadline. If no data are read
// before the timeout expires Read returns with err == ErrTimeout (and
// n == 0). If the read(2) system-call returns 0, Read returns with
// err = io.EOF (and n == 0).
func (fd *FD) Read(p []byte) (n int, err error) {
	if err = fd.readLock(); err != nil {
		return 0, err
	}
	defer fd.readUnlock()
	if err = fd.pd.PrepareRead(); err != nil {
		return 0, err
	}
	for {
		n, err = syscall.Read(int(fd.sysfd), p)
		if err != nil {
			n = 0
			if err != syscall.EAGAIN {
				break
			}
			if err = fd.pd.WaitRead(); err != nil {
				break
			}
			continue
		}
		if n == 0 && len(p) > 0 {
			err = io.EOF
		}
		break
	}
	return n, err
}

// Write writes len(p) bytes from p to the file-descriptor.  It
// returns the number of bytes written from p (0 <= n <= len(p)) and
// any error encountered that caused the write to stop early.  Write
// returns a non-nil error if it returns n < len(p). Write is formally
// and semantically compatible with the Write method of the io.Writer
// interface. See documentation of this interface method for more
// details. In addition Write honors the timeout set by
// (*FD).SetDeadline and (*FD).SetWriteDeadline. If less than len(p)
// data are writen before the timeout expires Write returns with err
// == ErrTimeout (and n < len(p)). If the write(2) system-call returns
// 0, Write returns with err == io.ErrUnexpectedEOF.
func (fd *FD) Write(p []byte) (nn int, err error) {
	if err := fd.writeLock(); err != nil {
		return 0, err
	}
	defer fd.writeUnlock()
	if err := fd.pd.PrepareWrite(); err != nil {
		return 0, err
	}
	for {
		var n int
		n, err = syscall.Write(fd.sysfd, p[nn:])
		if err != nil {
			n = 0
			if err != syscall.EAGAIN {
				break
			}
			err = fd.pd.WaitWrite()
			if err != nil {
				break
			}
			continue
		}
		if n == 0 {
			err = io.ErrUnexpectedEOF
			break
		}
		nn += n
		if nn == len(p) {
			break
		}
	}
	return nn, err
}

// SetDeadline sets the deadline for both Read and Write operations on
// the file-descriptor. Deadlines are expressed as ABSOLUTE instances
// in time. Example: To set a timeout 5 seconds in the future do:
//
//   fd.SetDeadline(time.Now().Add(5 * time.Second))
//
// This is equivalent to:
//
//   fd.SetReadDeadline(time.Now().Add(5 * time.Second))
//   fd.SetWriteDeadline(time.Now().Add(5 * time.Second))
//
// A zero value for t, cancels (removes) the existing deadline:
//
//   fd.SetDeadline(time.Time{})
//
func (fd *FD) SetDeadline(t time.Time) error {
	if err := fd.Incref(); err != nil {
		return err
	}
	fd.pd.SetDeadline(t, 'r'+'w')
	fd.Decref()
	return nil
}

// SetReadDeadline sets the deadline for Read operations on the
// file-descriptor. The deadline is expressed as an absolute instance
// in time. See also: (*FD).SetDeadline.
func (fd *FD) SetReadDeadline(t time.Time) error {
	if err := fd.Incref(); err != nil {
		return err
	}
	fd.pd.SetDeadline(t, 'r')
	fd.Decref()
	return nil
}

// SetWriteDeadline sets the deadline for Write operations on the
// file-descriptor. The deadline is expressed as an absolute instance
// in time. See also: (*FD).SetDeadline.
func (fd *FD) SetWriteDeadline(t time.Time) error {
	if err := fd.Incref(); err != nil {
		return err
	}
	fd.pd.SetDeadline(t, 'w')
	fd.Decref()
	return nil
}
