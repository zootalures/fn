package main

import (
	"github.com/fnproject/fn/api/broker"
	"fmt"
	"crypto/tls"
	"crypto/x509"
	"io/ioutil"
	"google.golang.org/grpc/credentials"
	"errors"
	"google.golang.org/grpc"
	"context"
	"net"
	"time"
	"log"
	"github.com/fnproject/fn/api/models"
	"io"
	"sync"
	"net/http"
	"github.com/fnproject/fn/api/agent"
)

var (
	crt = "client.crt"
	key = "client.key"
	ca  = "ca.crt"
)

// the standard grpc dial does not block on connection failures and hence completely hides all TLS errors
func BlockingDial(ctx context.Context, address string, creds credentials.TransportCredentials, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
	// grpc.Dial doesn't provide any information on permanent connection errors (like
	// TLS handshake failures). So in order to provide good error messages, we need a
	// custom dialer that can provide that info. That means we manage the TLS handshake.
	result := make(chan interface{}, 1)

	writeResult := func(res interface{}) {
		// non-blocking write: we only need the first result
		select {
		case result <- res:
		default:
		}
	}

	dialer := func(address string, timeout time.Duration) (net.Conn, error) {
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		conn, err := (&net.Dialer{Cancel: ctx.Done()}).Dial("tcp", address)
		if err != nil {
			writeResult(err)
			return nil, err
		}
		if creds != nil {
			conn, _, err = creds.ClientHandshake(ctx, address, conn)
			if err != nil {
				writeResult(err)
				return nil, err
			}
		}
		return conn, nil
	}

	// Even with grpc.FailOnNonTempDialError, this call will usually timeout in
	// the face of TLS handshake errors. So we can't rely on grpc.WithBlock() to
	// know when we're done. So we run it in a goroutine and then use result
	// channel to either get the channel or fail-fast.
	go func() {
		opts = append(opts,
			grpc.WithBlock(),
			grpc.FailOnNonTempDialError(true),
			grpc.WithDialer(dialer),
			grpc.WithInsecure(), // we are handling TLS, so tell grpc not to
		)
		conn, err := grpc.DialContext(ctx, address, opts...)
		var res interface{}
		if err != nil {
			res = err
		} else {
			res = conn
		}
		writeResult(res)
	}()

	select {
	case res := <-result:
		if conn, ok := res.(*grpc.ClientConn); ok {
			return conn, nil
		} else {
			return nil, res.(error)
		}
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func Connect(addr string) (broker.BrokerClient, error) {

	// Load the client certificates from disk
	certificate, err := tls.LoadX509KeyPair(crt, key)
	if err != nil {
		return nil, fmt.Errorf("could not load client key pair: %s", err)
	}

	// Create a certificate pool from the certificate authority
	certPool := x509.NewCertPool()
	ca, err := ioutil.ReadFile(ca)
	if err != nil {
		return nil, fmt.Errorf("could not read ca certificate: %s", err)
	}

	// Append the certificates from the CA
	if ok := certPool.AppendCertsFromPEM(ca); !ok {
		return nil, errors.New("failed to append ca certs")
	}

	creds := credentials.NewTLS(&tls.Config{
		ServerName:   "127.0.0.1", // NOTE: this is required!
		Certificates: []tls.Certificate{certificate},
		RootCAs:      certPool,
	})

	ctx := context.Background()
	// Create a connection with the TLS credentials
	conn, err := BlockingDial(ctx, addr, creds)
	if err != nil {
		return nil, fmt.Errorf("could not dial %s: %s", addr, err)
	}

	// Initialize the client and make the request
	client := broker.NewBrokerClient(conn)
	return client, nil
}

type callState struct {
	ID            string
	input         io.WriteCloser
	call          *models.Call
	outHeaders    http.Header
	outStatus     int
	headerWritten bool
	output        io.ReadCloser
	client        broker.Broker_EngageClient
}

type remoteRunner struct {
	callState sync.Map
	agent     agent.Agent
}

func (*remoteRunner) GetApp(ctx context.Context, appName string) (*models.App, error) {
	panic("should never be called")
	return nil, nil

}

func (*remoteRunner) GetRoute(ctx context.Context, appName string, routePath string) (*models.Route, error) {
	panic("should never be called")

	return nil, nil
}

// Enqueue will add a Call to the queue (ultimately forwards to mq.Push).
func (*remoteRunner) Enqueue(ctx context.Context, mCall *models.Call) error {
	panic("Can't enqueue")
}

func (*remoteRunner) Dequeue(ctx context.Context) (*models.Call, error) {
	// needs to block indefinitely
	// do we need to support async within the runner or in the broker?
	block := make(chan bool)
	<-block
	return nil, nil
}

func (*remoteRunner) Start(ctx context.Context, mCall *models.Call) error {
	log.Printf("Call Starting  %v", mCall)

	return nil
}

func (r *remoteRunner) Finish(ctx context.Context, mCall *models.Call, stderr io.Reader, async bool) error {
	log.Printf("Call complete %v", mCall)
	buf := make([]byte, 65535)
	v, ok := r.callState.Load(mCall.ID)
	r.callState.Delete(mCall.ID)

	if !ok {
		log.Fatalf("Unknown call %s", mCall.ID)
	}
	callState := v.(*callState)

	callState.client.Send(&broker.ClientMsg{
		Body: &broker.ClientMsg_CallComplete{
			CallComplete: &broker.CallComplete{
				CallId: mCall.ID,
			},
		},
	})

	for {
		count, err := stderr.Read(buf)
		eof := err == io.EOF
		if err != nil && !eof {
			return errors.New("failed to read log")
		}

		sendErr := callState.client.Send(&broker.ClientMsg{
			Body: &broker.ClientMsg_Log{
				Log: &broker.DataFrame{
					CallId: mCall.ID,
					Eof:    err == io.EOF,
					Data:   buf[:count],
				},
			},
		})

		if sendErr != nil {
			return errors.New("Failed to remote logs ")
		}
		if eof {
			break
		}

	}
	return nil
}

func (r *remoteRunner) handleData(data *broker.DataFrame) {
	log.Printf("Got data for %s eof? %s", data.CallId, data.Eof)
	v, ok := r.callState.Load(data.CallId)
	if !ok {
		log.Fatal("Got data for unexpected call ", data.CallId)
	}

	callData := v.(*callState)
	_, err := callData.input.Write(data.Data)
	if err != nil {
		log.Fatal("error writing to call")
	}

	if data.Eof {
		callData.input.Close()
	}
}

func (c *callState) Header() http.Header {
	return c.outHeaders
}

func (c *callState) WriteHeader(status int) {
	c.outStatus = status
	c.commitHeaders()
}

func (c *callState) commitHeaders() {

	if c.headerWritten {
		return
	}
	c.headerWritten = true
	log.Printf("committing call with headers  %v : %d", c.outHeaders, c.outStatus)

	var outHeaders []*broker.HttpHeader

	for h, vals := range c.outHeaders {
		for _, v := range vals {
			outHeaders = append(outHeaders, &broker.HttpHeader{
				Key:   h,
				Value: v,
			})
		}
	}

	log.Print("sending response value")

	err := c.client.Send(&broker.ClientMsg{
		Body: &broker.ClientMsg_CallRespond{
			CallRespond: &broker.CallResponse{
				CallId:      c.ID,
				ContentType: c.outHeaders.Get("Content-Type"),
				Meta: &broker.CallResponse_Http{
					Http: &broker.HttpRespMeta{
						Headers:    outHeaders,
						StatusCode: int32(c.outStatus),
					},
				},
				Eof: false,
			},
		},
	})

	if err != nil {
		log.Println("error sending call", err)
		panic(err)
	}
	log.Println("Sent call response ")
}

func (c *callState) Write(data []byte) (int, error) {
	log.Printf("Got response data %s", string(data))
	c.commitHeaders()
	err := c.client.Send(&broker.ClientMsg{
		Body: &broker.ClientMsg_Data{
			Data: &broker.DataFrame{
				CallId: c.ID,
				Data:   data,
			},
		},
	})

	if err != nil {
		return 0, errors.New("error sending data")
	}
	return len(data), nil
}

func (c *callState) Close() error {
	log.Printf("closing call %s", c.ID)
	c.commitHeaders()
	err := c.client.Send(&broker.ClientMsg{
		Body: &broker.ClientMsg_Data{
			Data: &broker.DataFrame{
				CallId: c.ID,
				Eof:    true,
			},
		},
	})

	if err != nil {
		return errors.New("error sending close frame")
	}
	return nil
}

func (r *remoteRunner) handleCall(client broker.Broker_EngageClient, call *broker.StartCall) {
	log.Printf("starting call %s", call.CallId)

	callData := &models.Call{
		ID:          call.CallId,
		Type:        call.Type,
		Format:      call.Format,
		Config:      call.ContainerConfig,
		Timeout:     call.Timeout,
		IdleTimeout: call.IdleTimeout,
		Image:       call.Image,
		Memory:      call.Memory,
		AppName:     call.App,
		Path:        call.Route,
	}

	callHttp := call.GetHttp()
	if callHttp != nil {
		callData.Method = callHttp.Method
		headers := http.Header{}
		for _, h := range callHttp.Headers {
			headers[h.Key] = append(headers[h.Key], h.Value)
		}
		callData.Headers = headers
		callData.URL = callHttp.RequestUrl
	}

	inR, inW := io.Pipe()

	rdCall := &callState{
		ID:         call.CallId,
		call:       callData,
		input:      inW,
		outStatus:  http.StatusOK,
		outHeaders: http.Header{},
		client:     client,
	}
	var w http.ResponseWriter
	w = rdCall

	agentCall, err := r.agent.GetCall(agent.FromModelAndPayload(callData, inR, w))

	if err != nil {
		panic(err)
	}
	r.callState.Store(call.CallId, rdCall)

	go func() {
		err = r.agent.Submit(agentCall)
		if err != nil {
			log.Println("error submitting call", err)
			r.callState.Delete(call.CallId)
			return
		}

		log.Printf("call submitted %s", call.CallId)

	}()

}

func runEngagement(client broker.BrokerClient) {
	// todo deal with stream disconnection/reconnection
	ctx := context.Background()
	engagement, err := client.Engage(ctx)

	if err != nil {
		panic(err)
	}
	engagement.Send(&broker.ClientMsg{
		Body: &broker.ClientMsg_Hello{
			Hello: &broker.ClientHello{
				ClientVersion: "1.0.0",
			},
		},
	})

	resp, err := engagement.Recv()

	if err != nil {
		log.Fatal("error in client", err)
	}
	olleh, ok := resp.Body.(*broker.ServerMsg_ServerHello)

	if !ok {
		log.Fatal("Got unexpected response from server, wanted olleh", resp)
	}

	log.Printf("got olleh from server: %v", olleh.ServerHello.ServerVersion)

	dataHolder := &remoteRunner{}

	grpc.EnableTracing = false
	dataHolder.agent = agent.New(dataHolder)

	log.Printf("entering runner loop")
	for {
		msg, err := engagement.Recv()
		if err != nil {
			log.Fatal("io error from server", err)
		}
		switch body := msg.Body.(type) {
		case *broker.ServerMsg_StartCall:
			dataHolder.handleCall(engagement, body.StartCall)
		case *broker.ServerMsg_Data:
			dataHolder.handleData(body.Data)
		}
	}
}

func main() {
	client, err := Connect("127.0.0.1:9190")

	if err != nil {
		panic(err)
	}

	runEngagement(client)
}
