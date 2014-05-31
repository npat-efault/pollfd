#pollfd [![GoDoc](https://godoc.org/github.com/npat-efault/pollfd?status.png)](https://godoc.org/github.com/npat-efault/pollfd)

>> **ATTENTION:** In order to build and use the pollfd package, you
>> must have a tweaked (non-standard) verion of the Go standard
>> library, that exports an interface to the netpoller mechanism. You
>> can download this from:
>> 
>> ```
>> https://code.google.com/r/nickpatavalis-pollfd/
>> ```
>> 
>> but, unless you just want to toy-around with it, DON'T!

>> Take a look instead at:
>>
>> >> https://github.com/npat-efault/poller]
>>
>> Which does, almost, the same thing without requiring special support
>> from the Go runtime.

**********

pollfd implements a file-descriptor type (FD) that can be used with
Go runtime's netpoll mechanism. A "pollfd.FD" is associated with a
system file-descriptor and can be used to read-from and write-to it
without forcing the runtime to create an OS-level thread for every
blocked operation. Blocked operations can, instead, be multiplexed
by the runtime on the same OS thread using the netpoller mechanism
(which is typically used for network connections). In addition, the
package provides support for timeouts on Read and Write operations.

pollfd is especially usefull for interfacing with terminals,
character devices, FIFOs, named pipes, etc.

Typical usage:

```
fd, err: = pollfd.Open("/dev/ttyXX", pollfd.O_RW)
if err != nil {
    log.Fatalln("Failed to open device:", err)
}

...

err = fd.SetReadDeadline(time.Now().Add(5 * time.Second))
if err != nil {
    log.Fatalln("Failed to set R deadline:", err)
}

...

b := make([]byte, 10)
n, err := fd.Read(b)
if err != nil {
    log.Fatalln("Write failed:", err)
}

...

n, err = fd.Write([]byte("Test"))
if err != nil {
    log.Fatalln("Write failed:", err)
}
```

Also, pollfd operations are thread-safe; the same FD can be used
from multiple go-routines. It is, for example, safe to close a file
descriptor blocked on a Read or Write call from another go-routine.
