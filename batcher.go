package batcher

import (
	"context"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"sync/atomic"
	"time"
)

type CommitFunc[T, R any] func(context.Context, []*Operation[T, R])

// UnlimitedSize Batch an unlimited amount of operations.
const UnlimitedSize = 0

// NoTimeout Batch operations for an infinite duration.
const NoTimeout time.Duration = 0

type Batcher[T, R any] struct {
	commitFn          CommitFunc[T, R]
	maxSize           int
	timeout           time.Duration
	in                chan *Operation[T, R]
	registry          prometheus.Registerer
	namespace         string
	subsystem         string
	batchSizeReached  uint64
	batchTimerReached uint64
}

// New creates a new batcher, calling the commit function each time it
// completes a batch of operations according to its options. It panics if the
// commit function is nil, max size is negative, timeout is negative or max
// size equals [UnlimitedSize] and timeout equals [NoTimeout] (the default if
// no options are provided).
//
// Some examples:
//
// Create a batcher committing a batch every 10 operations:
//
//	New[T, R](commitFn, WithMaxSize(10))
//
// Create a batcher committing a batch 1 second after receiving the first
// operation:
//
//	New[T, R](commitFn, WithTimeout(1 * time.Second))
//
// Create a batcher committing a batch containing at most 10 operations and at
// most 1 second after receiving the first operation:
//
//	New[T, R](commitFn, WithMaxSize(10), WithTimeout(1 * time.Second))
func New[T, R any](commitFn CommitFunc[T, R], opts ...Option[T, R]) *Batcher[T, R] {
	b := &Batcher[T, R]{
		commitFn: commitFn,
		maxSize:  UnlimitedSize,
		timeout:  NoTimeout,
		in:       make(chan *Operation[T, R]),
	}

	for _, opt := range opts {
		opt(b)
	}

	if b.commitFn == nil {
		panic("batcher: nil commit func")
	}

	if b.maxSize < 0 {
		panic("batcher: negative max size")
	}

	if b.timeout < 0 {
		panic("batcher: negative timeout")
	}

	if b.maxSize == UnlimitedSize && b.timeout == NoTimeout {
		panic("batcher: unlimited size with no timeout")
	}

	promauto.With(b.registry).NewCounterFunc(
		prometheus.CounterOpts{
			Name: prometheus.BuildFQName(b.namespace, b.subsystem, "batch_size_reached_total"),
			Help: "Number of batchs that reached batch size.",
		},
		func() float64 {
			return float64(atomic.LoadUint64(&b.batchSizeReached))
		})

	promauto.With(b.registry).NewCounterFunc(
		prometheus.CounterOpts{
			Name: prometheus.BuildFQName(b.namespace, b.subsystem, "batch_timer_reached_total"),
			Help: "Number of batchs that reached timer limit.",
		},
		func() float64 {
			return float64(atomic.LoadUint64(&b.batchTimerReached))
		})

	return b
}

// Send creates a new operation and sends it to the batcher in a blocking
// fashion. If the provided context expires before the batcher receives the
// operation, Send returns the context's error.
func (b *Batcher[T, R]) Send(ctx context.Context, v T) (*Operation[T, R], error) {
	op := newOperation[T, R](v)
	select {
	case b.in <- op:
		return op, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Start receives operations from the batcher, calling the commit function
// whenever max size is reached or a timeout occurs. Timeouts are disabled
// while receiving the first operation of each batch.
//
// When the provided context expires, the batching process is interrupted and
// the function returns after a final call to the commit function. The latter
// is skipped if there are no latent operations.
func (b *Batcher[T, R]) Start(ctx context.Context) {
	var out []*Operation[T, R]
	if b.maxSize != UnlimitedSize {
		out = make([]*Operation[T, R], 0, b.maxSize)
	}

	var (
		t *time.Timer
		c <-chan time.Time
	)

	for {
		var commit, done bool
		select {
		case op := <-b.in:
			out = append(out, op)
			if len(out) == b.maxSize {
				commit = true
				atomic.AddUint64(&b.batchSizeReached, 1)
			}
		case <-c:
			commit = true
			atomic.AddUint64(&b.batchTimerReached, 1)
		case <-ctx.Done():
			if len(out) > 0 {
				commit = true
			}
			done = true
		}

		if commit {
			b.commitFn(ctx, out)

			c = nil
			out = out[:0]
		}

		if done {
			break
		}

		if !commit && c == nil && b.timeout != NoTimeout {
			if t == nil {
				t = time.NewTimer(b.timeout)
			} else {
				t.Reset(b.timeout)
			}
			c = t.C
		}
	}
}
