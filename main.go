package main

import (
	"bufio"
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/golang/snappy"
	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/prompb"
)

const maxErrMsgLen = 256

var version = "1.0.0"
var userAgent = fmt.Sprintf("CustomClient/%v", version)

// This is an example of how to build prometheus an array of time series for remote write
func buildExampleWriteRequest() []*prompb.TimeSeries {
	return []*prompb.TimeSeries{
		{
			Labels: []*prompb.Label{
				{Name: "__name__", Value: "random_metric"}, // This is required and how prom stores the name of metrics
				{Name: "example_label_name", Value: "example_label_value"},
			},
			Samples: []prompb.Sample{
				{Value: 1, Timestamp: time.Now().Add(-time.Second).Unix() * 1000}, // Timestamps must be an int64 of millisecond unix time
				{Value: 1, Timestamp: time.Now().Unix() * 1000},
			},
		},
	}
}

// How to marshal the remote write time ser
func marshalWriteRequest(samples []*prompb.TimeSeries) ([]byte, error) {
	req := &prompb.WriteRequest{
		Timeseries: samples,
	}

	data, err := proto.Marshal(req)
	if err != nil {
		return nil, err
	}

	compressed := snappy.Encode(nil, data)
	return compressed, nil
}

// Client allows reading and writing from/to a remote HTTP endpoint.
type Client struct {
	client *http.Client
	id     string
	key    string
}

// Store sends a batch of samples to the grafana hosted metrics
func (c *Client) Store(req []byte) error {
	httpReq, err := http.NewRequest("POST", "https://prometheus-us-central1.grafana.net/api/prom/push", bytes.NewReader(req))
	if err != nil {
		// Errors from NewRequest are from unparseable URLs, so are not
		// recoverable.
		return err
	}
	httpReq.Header.Add("Content-Encoding", "snappy")
	httpReq.Header.Set("Content-Type", "application/x-protobuf")
	httpReq.Header.Set("User-Agent", userAgent)
	httpReq.Header.Set("X-Prometheus-Remote-Write-Version", "0.1.0")

	httpReq.SetBasicAuth(c.id, c.key)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*15)
	defer cancel()

	httpResp, err := c.client.Do(httpReq.WithContext(ctx))
	if err != nil {
		return err
	}
	defer func() {
		io.Copy(ioutil.Discard, httpResp.Body)
		httpResp.Body.Close()
	}()

	if httpResp.StatusCode/100 != 2 {
		scanner := bufio.NewScanner(io.LimitReader(httpResp.Body, maxErrMsgLen))
		line := ""
		if scanner.Scan() {
			line = scanner.Text()
		}
		err = errors.Errorf("server returned HTTP status %s: %s", httpResp.Status, line)
	}
	if httpResp.StatusCode/100 == 5 {
		return err
	}
	return err
}

func checkSet(s string) bool {
	if s == "" {
		return false
	}
	return true
}

func main() {
	id := flag.String("instance.id", "", "instance id of the desired hosted metrics instance")
	key := flag.String("api.key", "", "api key for the desired hosted metrics instance")
	flag.Parse()

	if !checkSet(*id) {
		fmt.Println("error: -instance.id not set")
		os.Exit(1)
	}

	if !checkSet(*key) {
		fmt.Println("error: -api.key not set")
		os.Exit(1)
	}

	cli := Client{
		client: http.DefaultClient,
		id:     *id,
		key:    *key,
	}

	req, err := marshalWriteRequest(buildExampleWriteRequest())
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	err = cli.Store(req)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
