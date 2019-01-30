package main

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/netdata/statsd"
)

const dev = false

// statusCodeReporter is a compatible `http.ResponseWriter` which stores the `statusCode` for further reporting.
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
	// We can't set chage status code after response written (net/http limitation, the default is 200)
	// Also,
	// changing the header map after a call to WriteHeader (or
	// Write) has no effect unless the modified headers are
	// trailers.
	w.written = true
	return w.ResponseWriter.Write(b)
}

func main() {
	statsWriter, err := statsd.UDP(":8125")
	if err != nil {
		panic(err)
	}

	statsD := statsd.NewClient(statsWriter, "hub.")
	statsD.FlushEvery(5 * time.Second)

	statsDMiddleware := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			path := r.URL.Path
			if len(path) == 1 {
				path = "-" // for root.
			} else if path == "/favicon.ico" {
				// Some clients like web browsers fires a connection to the $host/favicon.ico automatically,
				// this is not handled by our file server, we serve favicon from within templates.
				// So just ignore that path for metrics.
				next.ServeHTTP(w, r)
				return
			} else {
				path = path[1:] // ignore first slash "/".
				// replace / with ".",
				// for example graphite puts them in "subdirectories" automatically.
				path = strings.Replace(path, "/", ".", -1)
			}

			statsD.Increment(fmt.Sprintf("%s.request", path))

			// This wraps the response writer in order to give the ability to "store" the status code that we wanna need later on to capture.
			// It has a cost as you can imagine but this is the only way to get the status code,
			// read the `statusCodeReporter` structure for more.
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
