package protocolutil

import (
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

func TestDumpRequestTo(t *testing.T) {
	baseCtx := context.Background()
	canceledCtx, cancel := context.WithCancel(baseCtx)
	cancel()

	// avoid reuse of the reader across tests.
	body := func() io.Reader {
		return strings.NewReader("request-body")
	}

	type args struct {
		ctx context.Context
		w   io.WriteCloser
		req *http.Request
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{"valid context - empty body", args{baseCtx, os.Stdout, mustRequest("GET", "/", nil)}, false},
		{"valid context - with body", args{baseCtx, os.Stdout, mustRequest("GET", "/", body())}, false},

		{"canceled context - empty body", args{canceledCtx, os.Stdout, mustRequest("GET", "/", nil)}, true},
		{"canceled context - with body", args{canceledCtx, os.Stdout, mustRequest("GET", "/", body())}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := DumpRequestTo(tt.args.ctx, tt.args.w, tt.args.req); (err != nil) != tt.wantErr {
				t.Errorf("DumpRequestTo() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func mustRequest(method, urlStr string, body io.Reader) *http.Request {
	req, err := http.NewRequest(method, urlStr, body)
	if err != nil {
		panic("new request must be valid")
	}
	return req
}
