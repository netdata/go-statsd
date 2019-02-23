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

const (
	// Count is the "c" Counting statsd metric type.
	Count string = "c"

	// Gauge is the "g" Gauges statsd metric type.
	Gauge string = "g"

	// Unique is the "s" Sets statsd metric type.
	Unique string = "s"

	// Set is an alias for `Unique`.
	Set = Unique

	// Time is the "ms" Timing statsd metric type.
	Time string = "ms"

	// Histogram is the "h" statsd metric type,
	// difference from `Time` metric type is that `Time` writes milleseconds.
	// Read more at: https://docs.netdata.cloud/collectors/statsd.plugin/
	Histogram string = "h"
)

var (
	// Duration accepts a duration and returns a string of the duration's millesecond.
	Duration = func(v time.Duration) string { return Int(int(v / time.Millisecond)) }

	// Int accepts an int and returns its string form.
	Int = func(v int) string { return Int64(int64(v)) }

	// Int8 accepts an int8 and returns its string form.
	Int8 = func(v int8) string { return Int64(int64(v)) }

	// Int16 accepts an int16 and returns its string form.
	Int16 = func(v int16) string { return Int64(int64(v)) }

	// Int32 accepts an int32 and returns its string form.
	Int32 = func(v int32) string { return Int64(int64(v)) }

	// Int64 accepts an int64 and returns its string form.
	Int64 = func(v int64) string { return strconv.FormatInt(v, 10) }

	// Uint accepts an uint and returns its string form.
	Uint = func(v uint) string { return Uint64(uint64(v)) }

	// Uint8 accepts an uint8 and returns its string form.
	Uint8 = func(v uint8) string { return Uint64(uint64(v)) }

	// Uint16 accepts an uint16 and returns its string form.
	Uint16 = func(v uint16) string { return Uint64(uint64(v)) }

	// Uint32 accepts an uint32 and returns its string form.
	Uint32 = func(v uint32) string { return Uint64(uint64(v)) }

	// Uint64 accepts an uint64 and returns its string form.
	Uint64 = func(v uint64) string { return strconv.FormatUint(v, 10) }

	// Float32 accepts a float32 and returns its string form.
	Float32 = func(v float32) string { return Float64(float64(v)) }

	// Float64 accepts a float64 and returns its string form.
	Float64 = func(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
)

// Client implements the StatsD Client.
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

const defaultMaxPacketSize = 1500

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
// The first input argument, "writeCloser", should be a value which completes the `io.WriteCloser`
// interface. It can be a UDP connection or a string buffer or even the stdout for testing.
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

// SetMaxPackageSize sets the max buffer size,
// when exceeds it flushes the metrics to the statsd server.
//
// Fast Ethernet (1432) - This is most likely for Intranets.
// Gigabit Ethernet (8932) - Jumbo frames can make use of this feature much more efficient.
// Commodity Internet (512) - If you are routing over the internet a value in this range will be reasonable.
// You might be able to go higher, but you are at the mercy of all the hops in your route.
//
// Read more at: https://github.com/etsy/statsd/blob/master/docs/metric_types.md#multi-metric-packets
// Defaults to 1500.
// See `FlushEvery` and `Flush` too.
func (c *Client) SetMaxPackageSize(maxPacketSize int) {
	if maxPacketSize <= 0 {
		return
	}

	c.mu.Lock()
	c.flush(-1)

	c.maxPacketSize = maxPacketSize
	c.buf = make([]byte, 0, maxPacketSize)
	c.mu.Unlock()
}

// SetFormatter accepts a function which accepts the full metric name and returns a formatted string.
// Optionally, defaults to nil.
func (c *Client) SetFormatter(fmt func(metricName string) string) {
	if fmt == nil {
		return
	}

	c.mu.Lock()
	c.flush(-1)

	c.metricNameFormatter = fmt
	c.mu.Unlock()
}

// FlushEvery accepts a duration which is used to create a new ticker
// which will flush the buffered metrics on each tick.
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
			c.Flush(-1)
		}
	}()
}

// IsClosed reports whether the client is closed or not.
func (c *Client) IsClosed() bool {
	if c == nil {
		return true
	}

	return atomic.LoadUint32(&c.closed) > 0
}

// Close terminates the client,  before closing it will try to write any pending metrics.
func (c *Client) Close() error {
	if c != nil && c.w != nil {
		atomic.StoreUint32(&c.closed, 1)

		c.mu.Lock()
		if c.flushTicker != nil {
			c.flushTicker.Stop()
		}
		c.flush(-1)
		c.mu.Unlock()

		return c.w.Close()
	}

	return nil
}

var rateSep = []byte("|@")

func appendMetric(dst []byte, prefix, metricName, value, typ string, rate float32) []byte {
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

// WriteMetric writes to the buffer a single metric.
// When metrics are "big" enough (see `SetMaxPacketSize`) then they will be flushed to the statsd server.
//
// The "metricName" input argument is the metric name (prefix is setted automatically if any).
//
// The "value" input argument is any string value, use the `Int`, `Int8`,`Int16`, `Int32`, `Int64`
// or `Uint`, `Uint8`, `Uint16`, `Uint32`, `Uint64` or `Float32`, `Float64` or `Duration` value helpers
// to convert a desired number to a string value.
// However if you are working on a custom statds server you may want to pass any supported value here.
//
// The "typ" input argument is the type of the statsd,
// i.e "c"(statsd.Count),"ms"(statsd.Time),"g"(statsd.Gauge) and "s"(`statsd.Unique`)
//
// The "rate" input argument is optional and defaults to 1.
// Use the `Client#Count`, `Client#Increment`, `Client#Gauge`, `Client#Unique`, `Client#Time`,
// `Client#Record` and `Client#Histogram` for common metrics instead.
func (c *Client) WriteMetric(metricName, value, typ string, rate float32) error {
	c.mu.Lock()
	err := c.writeMetric(metricName, value, typ, rate)
	c.mu.Unlock()

	return err
}

func (c *Client) writeMetric(metricName, value, typ string, rate float32) error {
	n := len(c.buf)

	if c.metricNameFormatter != nil {
		metricName = c.metricNameFormatter(metricName)
	}

	if metricName == "" { // ignore if metric name is empty (after end-dev defined formatter executed).
		return nil
	}

	if typ == Gauge && len(value) > 1 && value[0] == '-' {
		// we can't explicitly set a gauge to a negative number
		// without first setting it to zero.
		err := c.writeMetric(metricName, "0", Gauge, rate)
		if err != nil {
			return err
		}
	}

	c.buf = appendMetric(c.buf, c.prefix, metricName, value, typ, rate)

	if len(c.buf) > c.maxPacketSize {
		err := c.flush(n)
		if err != nil {
			return err
		}
	}

	return nil
}

// Flush can be called manually, when `FlushEvery` is not configured, to flush the buffered metrics to the statsd server.
// Negative or zero "n" value will flush everything from the buffer.
// See `SetMaxPacketSize` too.
func (c *Client) Flush(n int) error {
	c.mu.Lock()
	err := c.flush(n)
	c.mu.Unlock()

	return err
}

func (c *Client) flush(n int) error {
	if len(c.buf) == 0 {
		return nil
	}

	if n <= 0 {
		n = len(c.buf)
	}

	_, err := c.w.Write(c.buf[:n-1] /* without last "\n" for udp but on tcp may be required, waiting for feedback */)
	if err != nil {
		return err
	}

	if n < len(c.buf) {
		copy(c.buf, c.buf[n:])
	}

	c.buf = c.buf[:len(c.buf)-n] // or written-1.
	return nil
}

// Count is a shortcut of `Client#WriteMetric(metricName, statsd.Int(value), statsd.Count, 1)`.
func (c *Client) Count(metricName string, value int) error {
	return c.WriteMetric(metricName, Int(value), Count, 1)
}

// Increment is a shortcut of `Client#Count(metricName, 1)`.
func (c *Client) Increment(metricName string) error {
	return c.Count(metricName, 1)
}

// Gauge is a shortcut of `Client#WriteMetric(metricName, statsd.Int(value), statsd.Gauge, 1)`.
func (c *Client) Gauge(metricName string, value int) error {
	return c.WriteMetric(metricName, Int(value), Gauge, 1)
}

// Unique is a shortcut of `Client#WriteMetric(metricName, statsd.Int(value), statsd.Unique, 1)`.
//
// Sampling rate is not supported on sets.
func (c *Client) Unique(metricName string, value int) error {
	return c.WriteMetric(metricName, Int(value), Unique, 1)
}

// Time is a shortcut of `Client#WriteMetric(metricName, statsd.Duration(value), statsd.Time, 1)`.
func (c *Client) Time(metricName string, value time.Duration) error {
	return c.WriteMetric(metricName, Duration(value), Time, 1)
}

// Record prepares a Timing metric which records a duration from now until the returned function is executed.
// For example:
// stop := client.Record("response.time."+ path, 1)
// next.ServeHTTP(w, r)
// stop() // This will write the metric of Timing with value of start time - stop time.
//
// Extremely useful to capture http delays.
func (c *Client) Record(metricName string, rate float32) func() error {
	start := time.Now()
	return func() error {
		dur := time.Now().Sub(start)
		return c.WriteMetric(metricName, Duration(dur), Time, rate)
	}
}

// Histogram writes a histogram metric value,
// difference from `Time` metric type is that `Time` writes milleseconds.
//
// Histogram is a shortcut of `Client#WriteMetric(metricName, value, statsd.Histogram, 1)`.
//
// Read more at: https://docs.netdata.cloud/collectors/statsd.plugin/
func (c *Client) Histogram(metricName string, value int) error {
	return c.WriteMetric(metricName, Int(value), Histogram, 1)
}
