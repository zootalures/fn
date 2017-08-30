package protocol

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/fnproject/fn/api/models"
	"github.com/fnproject/fn/api/runner/common"
	"github.com/fnproject/fn/api/runner/task"
)

// JSONInput this is the what's sent into the function
type JSONInput struct {
	RequestURL string `json:"request_url"`
	CallID     string `json:"call_id"`
	Method     string `json:"method"`
	Body       string `json:"body"`
}

// JSONOutput function must return this format
type JSONOutput struct {
	Body string `json:"body"`
}

// JSONProtocol converts stdin/stdout streams from HTTP into JSON format.
type JSONProtocol struct {
	in  io.Writer
	out io.Reader
}

func (p *JSONProtocol) IsStreamable() bool {
	return true
}

func (p *JSONProtocol) Dispatch(ctx context.Context, cfg *task.Config) error {
	ctx, log := common.LoggerWithStack(ctx, "JSON.Dispatch")
	log.Println("In JSON Dispatch")
	var retErr error
	done := make(chan struct{})
	go func() {
		// TODO not okay. plumb content-length from req into cfg..
		var body bytes.Buffer
		io.Copy(&body, cfg.Stdin)
		// convert to JSON func format
		jin := &JSONInput{
			RequestURL: cfg.RequestURL,
			Method:     cfg.Method,
			CallID:     cfg.ID,
			Body:       body.String(),
		}
		b, err := json.Marshal(jin)
		if err != nil {
			// this shouldn't happen
			log.Errorf("error marshalling JSONInput: %v", err)
			retErr = fmt.Errorf("error marshalling JSONInput: %v", err)
			return
		}
		p.in.Write(b)

		// TODO: put max size on how big the response can be so we don't blow up
		jout := &JSONOutput{}
		dec := json.NewDecoder(p.out)
		if err := dec.Decode(jout); err != nil {
			// log.Errorf("error unmarshalling JSONOutput: %v", err)
			// TODO: how do we get an error back to the client??
			retErr = fmt.Errorf("error unmarshalling JSONOutput: %v", err)
			return
		}

		// res := &http.Response{}
		// res.Body = strings.NewReader(jout.Body)
		// TODO: shouldn't we pass back the full response object or something so we can set some things on it here?
		// For instance, user could set response content type or what have you.
		io.Copy(cfg.Stdout, strings.NewReader(jout.Body))
		done <- struct{}{}
	}()

	timeout := time.After(cfg.Timeout)

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timeout:
		return models.ErrRunnerTimeout
	case <-done:
		return retErr
	}
}
