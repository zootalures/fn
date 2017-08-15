package protocolutil

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"strings"
)

// DumpRequestTo reads the given request in its HTTP/1.x wire representation and
// write the result to the given writer. The returned representation is an
// approximation only; some details of the initial request are lost while
// parsing it into an http.Request. In particular, the order and case of header
// field names are lost. The order of values in multi-valued headers is kept
// intact.
//
// If DumpRequestTo returns an error, the state of req is undefined.
func DumpRequestTo(ctx context.Context, w io.WriteCloser, req *http.Request) error {
	var b io.WriteCloser = &contextWriteCloser{ctx, w}
	var err error

	// By default, print out the unmodified req.RequestURI, which
	// is always set for incoming server requests. But because we
	// previously used req.URL.RequestURI and the docs weren't
	// always so clear about when to use DumpRequest vs
	// DumpRequestOut, fall back to the old way if the caller
	// provides a non-server Request.
	reqURI := req.RequestURI
	if reqURI == "" {
		reqURI = req.URL.RequestURI()
	}

	fmt.Fprintf(b, "%s %s HTTP/%d.%d\r\n", valueOrDefault(req.Method, "GET"),
		reqURI, req.ProtoMajor, req.ProtoMinor)

	absRequestURI := strings.HasPrefix(req.RequestURI, "http://") || strings.HasPrefix(req.RequestURI, "https://")
	if !absRequestURI {
		host := req.Host
		if host == "" && req.URL != nil {
			host = req.URL.Host
		}
		if host != "" {
			fmt.Fprintf(b, "Host: %s\r\n", host)
		}
	}

	chunked := len(req.TransferEncoding) > 0 && req.TransferEncoding[0] == "chunked"
	if len(req.TransferEncoding) > 0 {
		fmt.Fprintf(b, "Transfer-Encoding: %s\r\n", strings.Join(req.TransferEncoding, ","))
	}
	if req.Close {
		fmt.Fprintf(b, "Connection: close\r\n")
	}

	io.WriteString(b, "\r\n")

	if req.Body != nil {
		var dest = b
		if chunked {
			dest = httputil.NewChunkedWriter(dest)
		}
		_, err = io.Copy(dest, &contextReader{ctx, req.Body})
		if chunked {
			dest.(io.Closer).Close()
			io.WriteString(b, "\r\n")
		}
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return err
	}
}

// valueOrDefault returns value if nonempty, def otherwise.
func valueOrDefault(value, def string) string {
	if value != "" {
		return value
	}
	return def
}

// contextReader wraps a io.Reader intercepting and rejecting Read() calls if
// the give context is canceled.
type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (cr *contextReader) Read(p []byte) (int, error) {
	select {
	case <-cr.ctx.Done():
		return 0, cr.ctx.Err()
	default:
		return cr.r.Read(p)
	}
}

// contextWriteCloser wraps a io.WriteClose intercepting and rejecting Write()
// and Close() calls if the give context is canceled.
type contextWriteCloser struct {
	ctx context.Context
	w   io.WriteCloser
}

func (cw *contextWriteCloser) Write(p []byte) (int, error) {
	select {
	case <-cw.ctx.Done():
		return 0, cw.ctx.Err()
	default:
		return cw.w.Write(p)
	}
}

func (cw *contextWriteCloser) Close() error {
	select {
	case <-cw.ctx.Done():
		return cw.ctx.Err()
	default:
		return cw.w.Close()
	}
}

/*
TODO: the actual interface isn't what we thought it was.
func readResponse(b io.Writer, res io.Reader, req *http.Request) error {
	_ = http.ReadResponse
	panic("not implemented")
}
*/
