# StatsD Client (Go)

**statsd** is a simple, efficient [StatsD](https://github.com/etsy/statsd) client written in [Go](https://golang.org).

[![build status](https://img.shields.io/travis/netdata/go-statsd/master.svg?style=flat-square)](https://travis-ci.org/netdata/go-statsd) [![report card](https://img.shields.io/badge/report%20card-a%2B-b13333.svg?style=flat-square)](http://goreportcard.com/report/netdata/go-statsd) [![release](https://img.shields.io/badge/release%20-0.1-0077b3.svg?style=flat-square)](https://github.com/netdata/go-statsd/releases) 

## Features

* Works great with [netdata](https://github.com/netdata/netdata)
* Supports *Counting*, *Sampling*, *Timing*, *Gauges*, *Sets* and *Histograms* out of the box
* Extendable: Ability to send custom metric values and types
* It is blazing fast and does not allocate unnecessary memory. Metrics are sent based on a customized packet size, manual `flushing` of buffered metrics is also an option
* Beautiful, easy to learn API
* Easy to test
* Protocol-agnostic

## Installation

The only requirement is the [Go Programming Language](https://golang.org/dl/).

```sh
$ go get -u github.com/netdata/statsd
```

## Quick start

**statsd** is simple, it won't take more than 10 minutes of your time to read all of its documentation via [godocs](https://godoc.org/github.com/netdata/statsd).

### API

```go
// NewClient returns a new StatsD client.
// The first input argument, "writeCloser", should be a value which implements  
// the `io.WriteCloser` interface.
// It can be a UDP connection or a string buffer or even STDOUT for testing.
// The second input argument, "prefix" can be empty
// but it is usually the app's name and a single dot.
NewClient(writeCloser io.WriteCloser, prefix string) *Client
```

```go
Client {
    SetMaxPackageSize(maxPacketSize int)
    SetFormatter(fmt func(metricName string) string)
    FlushEvery(dur time.Duration)

    IsClosed() bool
    Close() error

    WriteMetric(metricName, value, typ string, rate float32) error
    Flush(n int) error

    Count(metricName string, value int) error
    Increment(metricName string) error

    Gauge(metricName string, value int) error

    Unique(metricName string, value int) error

    Time(metricName string, value time.Duration) error
    Record(metricName string, rate float32) func() error

    Histogram(metricName string, value int) error
}
```

#### Metric Value helpers

```go
Duration(v time.Duration) string
Int(v int) string
Int8(v int8) string
Int16(v int16) string
Int32(v int32) string
Int64(v int64) string
Uint(v uint) string
Uint8(v uint8) string
Uint16(v uint16) string
Uint32(v uint32) string
Uint64(v uint64) string
Float32(v float32) string
Float64(v float64) string
```

#### Metric Type constants

```go
const (
    // Count is the "c" Counting statsd metric type.
    Count string = "c"
    // Gauge is the "g" Gauges statsd metric type.
    Gauge string = "g"
    // Unique is the "s" Sets statsd metric type.
    Unique string = "s"
    // Set is an alias for "Unique"
    Set = Unique
    // Time is the "ms" Timing statsd metric type.
    Time string = "ms"
    // Histogram is the "h" metric type,
    // difference from `Time` metric type is that `Time` writes milleseconds.
    // Read more at: https://docs.netdata.cloud/collectors/statsd.plugin/
    Histogram string = "h"
)
```

> Read more at: https://github.com/etsy/statsd/blob/master/docs/metric_types.md

### Example

Assuming you have a [statsd server](https://github.com/etsy/statsd) running at `:8125` (default port).

```sh
# assume the following codes in example.go file
$ cat example.go
```

```go
package main

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/netdata/statsd"
)


// statusCodeReporter is a compatible `http.ResponseWriter`
// which stores the `statusCode` for further reporting.
type statusCodeReporter struct {
    http.ResponseWriter
    written    bool
    statusCode int
}

func (w *statusCodeReporter) WriteHeader(statusCode int) {
    if w.written {
        return
    }

    w.statusCode = statusCode
    w.ResponseWriter.WriteHeader(statusCode)
}

func (w *statusCodeReporter) Write(b []byte) (int, error) {
    w.written = true
    return w.ResponseWriter.Write(b)
}

func main() {
    statsWriter, err := statsd.UDP(":8125")
    if err != nil {
        panic(err)
    }

    statsD := statsd.NewClient(statsWriter, "prefix.")
    statsD.FlushEvery(5 * time.Second)

    statsDMiddleware := func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            path := r.URL.Path
            if len(path) == 1 {
                path = "index" // for root.
            } else if path == "/favicon.ico" {
                next.ServeHTTP(w, r)
                return
            } else {
                path = path[1:]
                path = strings.Replace(path, "/", ".", -1)
            }

            statsD.Increment(fmt.Sprintf("%s.request", path))

            newResponseWriter := &statusCodeReporter{ResponseWriter: w, statusCode: http.StatusOK}

            stop := statsD.Record(fmt.Sprintf("%s.time", path), 1)
            next.ServeHTTP(newResponseWriter, r)
            stop()

            statsD.Increment(fmt.Sprintf("%s.response.%d", path, newResponseWriter.statusCode))
        })
    }

    mux := http.DefaultServeMux

    mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
        fmt.Fprintln(w, "Hello from index")
    })

    mux.HandleFunc("/other", func(w http.ResponseWriter, r *http.Request) {
        statsD.Unique("other.unique", 1)
        fmt.Fprintln(w, "Hello from other page")
    })

    http.ListenAndServe(":8080", statsDMiddleware(mux))
}
```

```
# run example.go and visit http://localhost:8080/other
$ go run example.go
```

Navigate [here](_examples) to see more examples.

## License

The go-statsd library is licensed under the MIT [License](LICENSE).
