// Package statsd implements is a client for https://github.com/etsy/statsd
package statsd

import (
	"io"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

//
// Types.
//
const (
	Count  string = "c"
	Gauge  string = "g"
	Unique string = "s"
	Set           = Unique // alias for Unique.
	Time   string = "ms"
)

//
// Values.
//
var (
	Duration = func(v time.Duration) string { return Int(int(v / time.Millisecond)) }
	Int      = func(v int) string { return Int64(int64(v)) }
	Int8     = func(v int8) string { return Int64(int64(v)) }
	Int16    = func(v int16) string { return Int64(int64(v)) }
	Int32    = func(v int32) string { return Int64(int64(v)) }
	Int64    = func(v int64) string { return strconv.FormatInt(v, 10) }
	Uint     = func(v uint) string { return Uint64(uint64(v)) }
	Uint8    = func(v uint8) string { return Uint64(uint64(v)) }
	Uint16   = func(v uint16) string { return Uint64(uint64(v)) }
	Uint32   = func(v uint32) string { return Uint64(uint64(v)) }
	Uint64   = func(v uint64) string { return strconv.FormatUint(v, 10) }
	Float32  = func(v float32) string { return Float64(float64(v)) }
	Float64  = func(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
)

//
// Metric helpers.
//

type Client struct {
	w                   io.WriteCloser
	prefix              string
	metricNameFormatter func(metricName string) string
	maxPacketSize       int

	// we could use something like that to both `Stop` the ticker (to avoid any leaks),
	// if `FlushEvery` on client connection `Close`.
	// closed        chan struct{}
	closed uint32

	buf         []byte
	mu          sync.Mutex   // mutex for `buf` and `flushTicker`.
	flushTicker *time.Ticker // it's a variable in order to be re-used so `EveryFlush` can be called to change the Flush duration.
}

const defaultMaxPacketSize = 1193

// UDP returns an `io.WriteCloser` from an `UDP` connection.
//
// The "addr" should be the full UDP address of form: HOST:PORT.
// Usage:
// conn, _ := UDP(":8125")
// NewClient(conn, "my_prefix.")
func UDP(addr string) (io.WriteCloser, error) {
	if addr == "" {
		addr = ":8125"
	}

	raddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, err
	}

	conn, err := net.DialUDP("udp", nil, raddr)
	if err != nil {
		return nil, err
	}

	return conn, nil
}

// NewClient returns a new StatsD client.
// The first input argument, "w", should be a value which completes the `io.WriteCloser`
// interface. It can be a UDP connection or a buffer for testing.
//
// The second input argument, "prefix" can be empty but it is usually the app's name + '.'.
//
// Example:
// conn, err := UDP(":8125")
// if err != nil { panic(err) }
// client := NewClient(conn, "my_prefix.")
// defer client.Close()
// client.FlushEvery(4 *time.Second)
// [...]
// err := client.WriteMetric("my_metric", Int(1), Count, 0.5)
// ^ increment by one, sample rate at 0.5
//
// Read more at: https://github.com/etsy/statsd/blob/master/docs/metric_types.md
func NewClient(writeCloser io.WriteCloser, prefix string) *Client {
	c := &Client{w: writeCloser, prefix: prefix}
	c.SetMaxPackageSize(defaultMaxPacketSize)

	return c
}

func (c *Client) SetMaxPackageSize(maxPacketSize int) {
	if maxPacketSize <= 0 {
		return
	}

	c.mu.Lock()
	if len(c.buf) > 0 {
		c.Flush(-1)
	}

	c.maxPacketSize = maxPacketSize
	c.buf = make([]byte, 0, maxPacketSize)
	c.mu.Unlock()
}

func (c *Client) SetFormatter(fmt func(metricName string) string) {
	if fmt == nil {
		return
	}

	c.mu.Lock()
	if len(c.buf) > 0 {
		c.Flush(-1)
	}

	c.metricNameFormatter = fmt
	c.mu.Unlock()
}

func (c *Client) FlushEvery(dur time.Duration) {
	if dur == 0 || c.IsClosed() {
		return
	}

	c.mu.Lock()
	if c.flushTicker != nil {
		c.flushTicker.Stop()
	}
	c.flushTicker = time.NewTicker(dur)
	c.mu.Unlock()

	go func() {
		for range c.flushTicker.C {
			c.mu.Lock()
			c.Flush(-1)
			c.mu.Unlock()
		}
	}()
}

func (c *Client) Flush(n int) error {
	if len(c.buf) == 0 {
		return nil
	}

	if n <= 0 {
		n = len(c.buf)
	}

	_, err := c.w.Write(c.buf[:n-1] /* without last "\n" */)
	if err != nil {
		return err
	}

	if n < len(c.buf) {
		copy(c.buf, c.buf[n:])
	}

	c.buf = c.buf[:len(c.buf)-n] // or written-1.
	return nil
}

func (c *Client) IsClosed() bool {
	if c == nil {
		return true
	}

	return atomic.LoadUint32(&c.closed) > 0
}

func (c *Client) Close() error {
	if c != nil && c.w != nil {
		//
		// c.buf stays.
		//
		atomic.StoreUint32(&c.closed, 1)
		return c.w.Close()
	}

	return nil
}

var rateSep = []byte("|@")

func AppendMetric(dst []byte, prefix, metricName, value, typ string, rate float32) []byte {
	dst = append(dst, prefix...)
	dst = append(dst, metricName...)
	dst = append(dst, ':')

	dst = append(dst, value...)
	dst = append(dst, '|')
	dst = append(dst, typ...)

	if rate != 1 {
		dst = append(dst, rateSep...)
		rateValue := strconv.FormatFloat(float64(rate), 'f', -1, 32)
		dst = append(dst, rateValue...)
	}

	dst = append(dst, '\n')
	return dst
}

func (c *Client) WriteMetric(metricName, value, typ string, rate float32) error {
	c.mu.Lock()
	n := len(c.buf)

	if c.metricNameFormatter != nil {
		metricName = c.metricNameFormatter(metricName)
	}

	if metricName == "" { // ignore if metric name is empty (after end-dev defined formatter executed).
		c.mu.Unlock()
		return nil
	}

	if typ == Gauge && len(value) > 1 && value[0] == '-' {
		// we can't explicitly set a gauge to a negative number
		// without first setting it to zero.
		err := c.WriteMetric(metricName, "0", Gauge, rate)
		if err != nil {
			c.mu.Unlock()
			return err
		}
	}

	c.buf = AppendMetric(c.buf, c.prefix, metricName, value, typ, rate)

	if len(c.buf) > c.maxPacketSize {
		err := c.Flush(n)
		if err != nil {
			c.mu.Unlock()
			return err
		}
	}

	c.mu.Unlock()

	return nil
}

func (c *Client) Record(metricName string, rate float32) func() error {
	start := time.Now()
	return func() error {
		dur := time.Now().Sub(start)
		return c.WriteMetric(metricName, Duration(dur), Time, rate)
	}
}

func (c *Client) Count(metricName string, value int) error {
	return c.WriteMetric(metricName, Int(value), Count, 1)
}

func (c *Client) Increment(metricName string) error {
	return c.Count(metricName, 1)
}

func (c *Client) Gauge(metricName string, value int) error {
	return c.WriteMetric(metricName, Int(value), Gauge, 1)
}

func (c *Client) Unique(metricName string, value int) error {
	return c.WriteMetric(metricName, Int(value), Unique, 1)
}

func (c *Client) Time(metricName string, value time.Duration) error {
	return c.WriteMetric(metricName, Duration(value), Time, 1)
}
