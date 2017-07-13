package main

import (
	"bytes"
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/Sirupsen/logrus"
)

// TODO automagically deploy a function from this

var client = &http.Client{Transport: &http.Transport{
	Dial: (&net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 2 * time.Minute,
	}).Dial,
	TLSClientConfig: &tls.Config{
		ClientSessionCache: tls.NewLRUClientSessionCache(8192),
	},
	TLSHandshakeTimeout:   10 * time.Second,
	MaxIdleConnsPerHost:   512,
	Proxy:                 http.ProxyFromEnvironment,
	MaxIdleConns:          512,
	IdleConnTimeout:       90 * time.Second,
	ExpectContinueTimeout: 1 * time.Second,
},
}

func main() {
	url := flag.String("url", "http://129.146.10.153:8080/r/yodawg/hello", "api url")
	threads := flag.Int("threads", 128, "number of threads to call a function")
	n := flag.Int("n", 10000, "number of times to call function (total = n/threads)")

	flag.Parse()

	ch := make(chan struct{}, 1024)

	for i := 0; i < *threads; i++ {
		go func() {
			work(*url, *n/(*threads), ch)
		}()
	}

	enn := *n
	for i := 1; i <= enn; i++ {
		<-ch
		fmt.Fprintf(os.Stdout, "%v / %v\r", i, enn)
	}
}

func work(url string, n int, done chan<- struct{}) {
	var b bytes.Buffer
	f := func() {
		resp, err := client.Get(url)
		if err != nil {
			logrus.WithError(err).Error("error calling function")
			return
		}

		b.Reset()
		io.Copy(&b, resp.Body)
		resp.Body.Close()

		if resp.StatusCode != 200 {
			logrus.WithFields(logrus.Fields{"code": resp.StatusCode, "body": b.String()}).Error("function failed")
		}
	}

	for i := 0; i < n; i++ {
		f()
		done <- struct{}{}
	}
}
