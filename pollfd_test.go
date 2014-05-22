package pollfd

import (
	"runtime"
	"syscall"
	"testing"
	"time"
)

// !! ATTENTION !! We unconditionally delete this file !!
const fifo = "/tmp/pollfd-test-fifo"

func mkfifo(t *testing.T) {
	_ = syscall.Unlink(fifo)
	err := syscall.Mkfifo(fifo, 0666)
	if err != nil {
		t.Fatal("mkfifo:", err)
	}
}

func TestIsError(t *testing.T) {
	if !IsErrorTemporary(ErrTimeout) {
		t.Fatal("ErrTimeout not temporary!")
	}
	if !IsErrorTimeout(ErrTimeout) {
		t.Fatal("ErrTimeout not timeout!")
	}
	if !IsErrorTemporary(syscall.Errno(syscall.EINTR)) {
		t.Fatal("EINTR not temporary!")
	}
	if IsErrorTimeout(syscall.Errno(syscall.EINTR)) {
		t.Fatal("EINTR is timeout!")
	}
	if IsErrorTemporary(ErrClosing) {
		t.Fatal("ErrClosing is temporary!")
	}
	if IsErrorTimeout(ErrClosing) {
		t.Fatal("ErrClosing is timeout!")
	}
	if IsErrorTemporary(syscall.Errno(syscall.EFAULT)) {
		t.Fatal("EFAULT is temporary!")
	}
	if IsErrorTimeout(syscall.Errno(syscall.EFAULT)) {
		t.Fatal("EFAULT is timeout!")
	}
}

func TestOpenClose(t *testing.T) {
	// /dev/null should be non-pollable
	_, err := Open("/dev/null", O_RO)
	if err == nil {
		t.Fatal("Sucessfully opened /dev/null!!")
	}
	// make and open FIFO
	mkfifo(t)
	fd, err := Open(fifo, O_RO)
	if err != nil {
		t.Fatal("Open:", err)
	}
	if fd.Name() != fifo {
		t.Fatal("fd.Name: ", fd.Name())
	}
	err = fd.Close()
	if err != nil {
		t.Fatal("Close:", err)
	}
}

func TestReadWrite(t *testing.T) {
	var b = make([]byte, 100)

	mkfifo(t)

	// Open read-end of pipe
	fdr, err := Open(fifo, O_RO)
	if err != nil {
		t.Fatal("Open read-side:", err)
	}

	// Open write-end of pipe
	fdw, err := Open(fifo, O_WO)
	if err != nil {
		t.Fatal("Open write-side:", err)
	}

	// Cause read-timeout
	err = fdr.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	if err != nil {
		t.Fatal("SetReadDeadline:", err)
	}
	_, err = fdr.Read(b)
	if err != ErrTimeout {
		t.Fatal("Expected:", ErrTimeout, "- Got:", err)
	}

	// Cause write timeout (after filling fifo buffer)
	err = fdw.SetWriteDeadline(time.Now().Add(1 * time.Second))
	if err != nil {
		t.Fatal("SetWriteDeadline:", err)
	}
	nw := 0
	// must be large enough to fill fifo buffer
	for i := 0; i < 1024*200; i++ {
		var n int
		n, err = fdw.Write([]byte("0123456789abcdef"))
		nw += n
		if err != nil {
			break
		}
	}
	if err != ErrTimeout {
		t.Fatal("Expected:", ErrTimeout, "- Got:", err)
	}

	// Read data in fifo buffer
	err = fdr.SetReadDeadline(time.Now().Add(1 * time.Second))
	if err != nil {
		t.Fatal("SetReadDeadline:", err)
	}
	for {
		nr := nw
		if nr > cap(b) {
			nr = cap(b)
		}
		n, err := fdr.Read(b[:nr])
		if err != nil {
			t.Fatal("Read:", err, nw)
		}
		nw -= n
		if nw == 0 {
			break
		}
	}

	// Try to read more and get a timeout
	err = fdr.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	if err != nil {
		t.Fatal("SetReadDeadline:", err)
	}
	_, err = fdr.Read(b[:1])
	if err != ErrTimeout {
		t.Fatal("Expected:", ErrTimeout, "- Got:", err)
	}

	done := make(chan bool)
	go func() {
		err = fdr.SetReadDeadline(time.Now().Add(1 * time.Second))
		if err != nil {
			done <- true
			t.Fatal("SetReadDeadline:", err)
		}
		_, err = fdr.Read(b[:1])
		if err != ErrClosing {
			done <- true
			t.Fatal("Expected:", ErrClosing, "- Got:", err)
		}
		done <- true
	}()
	time.Sleep(500 * time.Millisecond)
	fdr.Close()
	<-done

	// Re-open read-end of pipe
	fdr, err = Open(fifo, O_RO)
	if err != nil {
		t.Fatal("Open read-side:", err)
	}

	// Test finalizer
	fdr = nil
	runtime.GC()
	time.Sleep(200 * time.Millisecond)
	err = fdw.SetWriteDeadline(time.Now().Add(100 * time.Millisecond))
	if err != nil {
		t.Fatal("SetWriteDeadline", err)
	}
	_, err = fdw.Write([]byte("01234567890abcdef"))
	if err != syscall.EPIPE {
		t.Fatal("Expected:", syscall.EPIPE, "- Got:", err)
	}

	// Close write-end and test Incref
	fdw.Close()
	err = fdw.Incref()
	if err != ErrClosing {
		t.Fatal("Expected:", ErrClosing, "- Got:", err)
	}
}

func TestClosed(t *testing.T) {
	mkfifo(t)
	fdr, err := Open(fifo, O_RO)
	if err != nil {
		t.Fatal("Open:", err)
	}
	err = fdr.Close()
	if err != nil {
		t.Fatal("Close:", err)
	}
	err = fdr.Close()
	if err != ErrClosing {
		t.Fatal("Expected:", ErrClosing, " - Got:", err)
	}
	b := make([]byte, 10)
	_, err = fdr.Read(b)
	if err != ErrClosing {
		t.Fatal("Expected:", ErrClosing, " - Got:", err)
	}
	_, err = fdr.Write(b)
	if err != ErrClosing {
		t.Fatal("Expected:", ErrClosing, " - Got:", err)
	}
	err = fdr.SetReadDeadline(time.Time{})
	if err != ErrClosing {
		t.Fatal("Expected:", ErrClosing, " - Got:", err)
	}
	err = fdr.SetWriteDeadline(time.Time{})
	if err != ErrClosing {
		t.Fatal("Expected:", ErrClosing, " - Got:", err)
	}
	err = fdr.SetDeadline(time.Time{})
	if err != ErrClosing {
		t.Fatal("Expected:", ErrClosing, " - Got:", err)
	}
	err = fdr.Incref()
	if err != ErrClosing {
		t.Fatal("Expected:", ErrClosing, " - Got:", err)
	}
}
