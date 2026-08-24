package extension

import (
	"bytes"
	"context"
	"io"
	"sync"
	"testing"
	"time"
)

// TestNotifyRaceWithServeShutdown is the regression test for the local
// deviation (review item L-17): the read loop closes notifyQueue on exit while
// notify() used to do a non-atomic check-then-send, so an in-flight send could
// panic with "send on closed channel". The test hammers notify() from
// concurrent goroutines while the read loop exits (EOF), then asserts that
// post-shutdown notify calls return an error instead of panicking. Run with
// -race to cover the interleaving.
func TestNotifyRaceWithServeShutdown(t *testing.T) {
	pr, pw := io.Pipe()
	var out bytes.Buffer // all writes go through writeFrame under wmu
	c := newConn(pr, &out, nil)

	serveDone := make(chan error, 1)
	go func() { serveDone <- c.serve(context.Background()) }()

	stop := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				// Errors are expected here (queue overflow shuts the
				// connection down; shutdown rejects sends); only a panic
				// is a failure, and it would crash the test binary.
				_ = c.notify("extension/provider/stream_chunk", map[string]string{"streamId": "s", "seq": "1"})
			}
		}()
	}
	// Let senders get in flight, then EOF the read loop so serve() closes
	// notifyQueue concurrently with in-flight sends.
	time.Sleep(20 * time.Millisecond)
	_ = pw.Close()
	select {
	case <-serveDone:
		// Serve may report a queue-overflow shutdown error; the test only
		// asserts the absence of a panic/race.
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not return after EOF")
	}
	close(stop)
	wg.Wait()

	// After shutdown, notify must fail cleanly (closedError), never panic.
	if err := c.notify("extension/provider/stream_end", map[string]string{"streamId": "s"}); err == nil {
		t.Fatal("notify after shutdown should return an error")
	}
}
