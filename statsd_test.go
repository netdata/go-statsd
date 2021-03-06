package statsd

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"
)

type ClosingBuffer struct {
	*bytes.Buffer
}

func (b *ClosingBuffer) Close() error {
	b.Buffer.Reset()
	return nil
}

func runTest(tester func(c *Client, w fmt.Stringer)) {
	w := &ClosingBuffer{new(bytes.Buffer)}
	client := NewClient(w, "my_prefix.")
	defer client.Close()

	tester(client, w)
}

func TestClientWriteMetric(t *testing.T) {
	runTest(func(c *Client, w fmt.Stringer) {
		err := c.WriteMetric("my_metric", Int64(9223372036854775807), Count, 1)
		if err != nil {
			t.Fatal(err)
		}

		err = c.WriteMetric("my_metric2", Float64(0.4), Gauge, 0.1)
		if err != nil {
			t.Fatal(err)
		}

		err = c.WriteMetric("my_metric3", Float64(-10), Gauge, 1)
		if err != nil {
			t.Fatal(err)
		}

		c.Flush(-1)

		if expected, got := "my_prefix.my_metric:9223372036854775807|c\nmy_prefix.my_metric2:0.4|g|@0.1\nmy_prefix.my_metric3:0|g\nmy_prefix.my_metric3:-10|g",
			w.String(); expected != got {
			t.Fatalf("expected:\n[%s]\nbut got:\n[%s]", expected, got)
		}
	})

}

func TestClientFlushEvery(t *testing.T) {
	w := &ClosingBuffer{new(bytes.Buffer)}
	client := NewClient(w, "")
	defer client.Close()
	err := client.WriteMetric("my_metric", Int(1), Count, 1)
	if err != nil {
		t.Fatal(err)
	}

	client.FlushEvery(2 * time.Second)

	if w.String() != "" {
		t.Fatalf("should not Flush yet")
	}

	time.Sleep(3 * time.Second)

	if got := w.String(); got != "my_metric:1|c" {
		t.Fatalf("expected other result here but got [%s]", got)
	}

	// test `Client#Flush` should not contain any old data.
	w.Reset()

	err = client.WriteMetric("my_metric2", Int(2), Count, 1)
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(3 * time.Second)

	if got := w.String(); got != "my_metric2:2|c" { //
		t.Fatalf("expected other result here but got [%s]", got)
	}
}

func TestClientRecord(t *testing.T) {
	w := &ClosingBuffer{new(bytes.Buffer)}
	client := NewClient(w, "")
	defer client.Close()

	stop := client.Record("http.response.time", 1)
	time.Sleep(1*time.Second + 100*time.Millisecond)
	stop()
	client.Flush(-1)

	expected := "http.response.time:1100|ms"
	if got := w.String(); len(got) != len(expected) {
		t.Fatalf("expected other record time but got [%s]", got)
	}
}

func TestClientMetricNameFormatter(t *testing.T) {
	w := &ClosingBuffer{new(bytes.Buffer)}
	client := NewClient(w, "http.request.path")
	client.SetFormatter(func(s string) string {
		s = strings.Replace(s, "/", "_", -1)
		s = strings.Replace(s, ":", "_", -1)
		return s
	})
	defer client.Close()

	err := client.Increment("/visit_me/here")
	if err != nil {
		t.Fatal(err)
	}

	client.Flush(-1)

	got := w.String()
	expected := "http.request.path_visit_me_here:1|c"
	if got != expected {
		t.Fatalf("expected to receive [%s] but got [%s]", expected, got)
	}
}

func BenchmarkClient(b *testing.B) {
	const testMetricName = "my_test_metric"
	w := &ClosingBuffer{new(bytes.Buffer)}
	client := NewClient(w, "")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		client.WriteMetric(testMetricName, Int(1), Count, 1)
		client.WriteMetric(testMetricName, Int(i), Gauge, 1)
		client.WriteMetric(testMetricName, Int(i), Unique, 1)
		client.WriteMetric(testMetricName, Int(i), Time, 1)
		client.Record(testMetricName, 1)()
	}
	client.Close()
}
