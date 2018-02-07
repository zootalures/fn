package main

import (
	"crypto/tls"
	"fmt"
	"crypto/x509"
	"io/ioutil"
	"google.golang.org/grpc/credentials"
	"net"
	"github.com/fnproject/fn/api/broker"
	"errors"
	"google.golang.org/grpc"
	"sync"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"log"
	"net/http"
	"github.com/gin-gonic/gin"
	"github.com/fnproject/fn/api/id"
	"github.com/fnproject/fn/api/agent/hybrid"
	"github.com/fnproject/fn/api/agent"
	"github.com/fnproject/fn/api/models"
	"io"
)

type callState struct {
	ID    string
	ctx   *gin.Context
	app   *models.App
	route *models.Route
	done  chan bool
}

type brokerClient struct {
	client    broker.Broker_EngageServer
	callState sync.Map
	id        string
}

type brokerState struct {
	gRPCServer  *grpc.Server
	clientState sync.Map
	dataAccess  agent.DataAccess
	listen      string
}

var (
	crt = "server.crt"
	key = "server.key"
	ca  = "ca.crt"
)

func (cs *callState) handleCallRespond(cr *broker.CallResponse) {
	log.Printf("got call response %v", cr)

	httpMeta := cr.Meta.(*broker.CallResponse_Http).Http

	for _, hv := range httpMeta.Headers {
		cs.ctx.Header(hv.Key, hv.Value)
	}
	cs.ctx.Status(int(httpMeta.StatusCode))

}
func (cs *callState) handleCallComplete(complete *broker.CallComplete) {
	log.Printf("Call done %s", cs.ID)
	cs.done <- true

}
func (cs *callState) handleCallData(frame *broker.DataFrame) {
	log.Printf("got call data %s: %s, %s", cs.ID, string(frame.Data), frame.Eof)

	cs.ctx.Writer.Write(frame.Data)

	if frame.Eof {
		log.Printf("Done!")
		cs.done <- true
		// end of frame
	}

}
func (cs *callState) handleLog(frame *broker.DataFrame) {

	log.Printf("Container Log %s: %v", cs.ID, frame)

}

// Handles a client engagement
func (bs *brokerState) Engage(engagement broker.Broker_EngageServer) error {

	pv, ok := peer.FromContext(engagement.Context())
	authInfo := pv.AuthInfo.(credentials.TLSInfo)
	clientCn := authInfo.State.PeerCertificates[0].Subject.CommonName
	log.Println("Got connection from", clientCn)

	if ok {
		log.Println("got peer ", pv)
	}
	md, ok := metadata.FromIncomingContext(engagement.Context())

	if ok {
		log.Println("got md ", md)
	}
	msg, err := engagement.Recv()
	if err != nil {
		return err
	}
	body := msg.GetBody()

	hello, ok := body.(*broker.ClientMsg_Hello)
	if !ok {
		return fmt.Errorf("unexpected client message - expecting hello , got %v", body)
	}

	log.Print("got hello", hello.Hello)
	engagement.Send(&broker.ServerMsg{
		Body: &broker.ServerMsg_ServerHello{
			ServerHello: &broker.ServerHello{
				ServerVersion: "1.0.0",
			},
		},
	})

	cs := &brokerClient{
		id:     clientCn,
		client: engagement,
	}

	_, loaded := bs.clientState.LoadOrStore(clientCn, cs)
	if loaded {
		return fmt.Errorf("client %s is already connected, cant connect again", clientCn)
	}

	defer bs.clientState.Delete(clientCn)

	for {
		msg, err := engagement.Recv()

		if err != nil {
			return err
		}
		switch body := msg.Body.(type) {
		case *broker.ClientMsg_CallComplete:
			v, ok := cs.callState.Load(body.CallComplete.CallId)
			callState := v.(*callState)
			if !ok {
				panic("no function")
			}
			callState.handleCallComplete(body.CallComplete)

		case *broker.ClientMsg_CallRespond:
			v, ok := cs.callState.Load(body.CallRespond.CallId)
			callState := v.(*callState)
			if !ok {
				panic("no function")
			}
			callState.handleCallRespond(body.CallRespond)

		case *broker.ClientMsg_Data:
			v, ok := cs.callState.Load(body.Data.CallId)
			callState := v.(*callState)
			if !ok {
				panic("no function")
			}
			callState.handleCallData(body.Data)

		case *broker.ClientMsg_Log:
			v, ok := cs.callState.Load(body.Log.CallId)
			callState := v.(*callState)
			if !ok {
				panic("no function")
			}
			callState.handleLog(body.Log)

		}

		if err != nil {
			return err
		}

	}
	return nil
}

func CreateBroker(addr string, dataAccess agent.DataAccess) (*brokerState, error) {
	// Load the certificates from disk
	certificate, err := tls.LoadX509KeyPair(crt, key)
	if err != nil {
		return nil, fmt.Errorf("could not load server key pair: %s", err)
	}

	// Create a certificate pool from the certificate authority
	certPool := x509.NewCertPool()
	ca, err := ioutil.ReadFile(ca)
	if err != nil {
		return nil, fmt.Errorf("could not read ca certificate: %s", err)
	}

	if ok := certPool.AppendCertsFromPEM(ca); !ok {
		return nil, errors.New("failed to append client certs")
	}

	creds := credentials.NewTLS(&tls.Config{
		ClientAuth:   tls.RequireAndVerifyClientCert,
		Certificates: []tls.Certificate{certificate},
		ClientCAs:    certPool,
	})

	srv := grpc.NewServer(grpc.Creds(creds))

	server := &brokerState{
		gRPCServer: srv,
		dataAccess: dataAccess,
		listen:     addr,
	}

	broker.RegisterBrokerServer(srv, server)

	return server, nil
}

func (bs *brokerState) Start() error {
	log.Println("Listening on ", bs.listen)
	lis, err := net.Listen("tcp", bs.listen)
	if err != nil {
		return fmt.Errorf("could not list on %s: %s", bs.listen, err)
	}

	if err := bs.gRPCServer.Serve(lis); err != nil {
		return fmt.Errorf("grpc serve error: %s", err)
	}
	return nil
}

func buildConfig(app *models.App, route *models.Route) models.Config {
	conf := make(models.Config, 8+len(app.Config)+len(route.Config))
	for k, v := range app.Config {
		conf[k] = v
	}
	for k, v := range route.Config {
		conf[k] = v
	}

	conf["FN_FORMAT"] = route.Format
	conf["FN_APP_NAME"] = app.Name
	conf["FN_PATH"] = route.Path
	// TODO: might be a good idea to pass in: "FN_BASE_PATH" = fmt.Sprintf("/r/%s", appName) || "/" if using DNS entries per app
	conf["FN_MEMORY"] = fmt.Sprintf("%d", route.Memory)
	conf["FN_TYPE"] = route.Type

	CPUs := route.CPUs.String()
	if CPUs != "" {
		conf["FN_CPUS"] = CPUs
	}
	return conf
}

func reqURL(req *http.Request) string {
	if req.URL.Scheme == "" {
		if req.TLS == nil {
			req.URL.Scheme = "http"
		} else {
			req.URL.Scheme = "https"
		}
	}
	if req.URL.Host == "" {
		req.URL.Host = req.Host
	}
	return req.URL.String()
}

// TODO copy pasta from agent/call.go
func FromRequest(da agent.DataAccess, appName string, path string, ctx *gin.Context) (*callState, *broker.StartCall, error) {
	req := ctx.Request
	app, err := da.GetApp(ctx, appName)
	if err != nil {
		return nil, nil, err
	}

	route, err := da.GetRoute(ctx, appName, path)
	if err != nil {
		return nil, nil, err
	}

	if route.Format == "" {
		route.Format = models.FormatDefault
	}

	id := id.New().String()

	//// TODO this relies on ordering of opts, but tests make sure it works, probably re-plumb/destroy headers
	//// TODO async should probably supply an http.ResponseWriter that records the logs, to attach response headers to
	//resp.outHeaders = append(outHeaders, &broker.HttpHeader{Key: "FN_CALL_ID", Value: id})
	//
	//for k, vs := range route.Headers {
	//	for _, v := range vs {
	//		outHeaders = append(outHeaders, &broker.HttpHeader{Key: k, Value: v})
	//	}
	//}

	var inHeaders []*broker.HttpHeader

	for k, vs := range req.Header {
		for _, v := range vs {
			inHeaders = append(inHeaders, &broker.HttpHeader{Key: k, Value: v})
		}
	}

	// this ensures that there is an image, path, timeouts, memory, etc are valid.
	// NOTE: this means assign any changes above into route's fields
	err = route.Validate()
	if err != nil {
		return nil, nil, err
	}
	call := &broker.StartCall{
		CallId:          id,
		App:             appName,
		Route:           route.Path,
		Image:           route.Image,
		Type:            route.Type,
		Format:          route.Format,
		Timeout:         route.Timeout,
		IdleTimeout:     route.IdleTimeout,
		Memory:          route.Memory,
		ContentType:     req.Header.Get("Content-type"),
		ContainerConfig: buildConfig(app, route),
		Proto: &broker.StartCall_Http{
			Http: &broker.HttpReqMeta{
				Headers:    inHeaders,
				RequestUrl: reqURL(req),
				Method:     req.Method,
			},
		},
	}
	callState := &callState{
		ID:    id,
		ctx:   ctx,
		app:   app,
		route: route,
		done:  make(chan bool),
	}

	return callState, call, nil
}
func (bs *brokerState) handleFunctionCall(ctx *gin.Context) {
	app := ctx.Param("app")
	route := ctx.Param("route")

	if route == "" {
		route = "/"
	}

	var client *brokerClient
	bs.clientState.Range(func(k interface{}, v interface{}) bool {
		client = v.(*brokerClient)
		return true
	})

	if client == nil {
		ctx.AbortWithError(504, errors.New("no clients available"))
		return
	}

	callState, start, err := FromRequest(bs.dataAccess, app, route, ctx)

	if err != nil {
		ctx.AbortWithError(504, err)
		return
	}
	log.Printf("Sending start call %v", start)
	err = client.client.Send(&broker.ServerMsg{
		Body: &broker.ServerMsg_StartCall{
			StartCall: start,
		},
	})

	if err != nil {
		ctx.AbortWithError(504, err)
		return
	}
	client.callState.Store(callState.ID, callState)

	body := ctx.Request.Body
	buf := make([]byte, 65535)
	for {

		n, err := body.Read(buf)
		eof := err == io.EOF
		if err != nil && !eof {
			ctx.AbortWithError(500, err)
			return
		}
		log.Printf("sending %d bytes of data %v", n, err)

		err = client.client.Send(&broker.ServerMsg{
			Body: &broker.ServerMsg_Data{
				Data: &broker.DataFrame{
					CallId: callState.ID,
					Data:   buf[:n],
					Eof:    err == io.EOF,
				},
			},
		})
		if eof {
			break
		}

	}
	<-callState.done

}

func main() {
	fnApi := "http://localhost:8080/"
	dataAccess, err := hybrid.NewClient(fnApi)
	if err != nil {
		log.Fatal(err)
	}

	bs, err := CreateBroker("localhost:9190", dataAccess)

	r := gin.Default()

	runner := r.Group("/r")
	runner.Any("/:app", bs.handleFunctionCall)
	runner.Any("/:app/*route", bs.handleFunctionCall)
	go func() {
		r.Run(":18080")

	}()

	if err != nil {
		log.Fatal(err)
	}
	bs.Start()

}
