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
)

type fneticServer struct {
	gRPCServer  *grpc.Server
	clientState sync.Map
}

func (server *fneticServer) completeCall(complete *broker.CallComplete) {

}

var (
	crt = "server.crt"
	key = "server.key"
	ca  = "ca.crt"
)

// Handles a client engagement
func (server *fneticServer) Engage(engagement broker.Broker_EngageServer) error {
	pv, ok := peer.FromContext(engagement.Context())

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

	for {
		msg, err := engagement.Recv()

		switch body := msg.Body.(type) {
		case *broker.ClientMsg_CallComplete:
			server.completeCall(body.CallComplete)
		case *broker.ClientMsg_CallRespond:
		case *broker.ClientMsg_Data:
		case *broker.ClientMsg_Log:

		}

		if err != nil {
			return err
		}

	}
	return nil
}

func startServer(addr string) error {
	// Load the certificates from disk
	certificate, err := tls.LoadX509KeyPair(crt, key)
	if err != nil {
		return fmt.Errorf("could not load server key pair: %s", err)
	}

	// Create a certificate pool from the certificate authority
	certPool := x509.NewCertPool()
	ca, err := ioutil.ReadFile(ca)
	if err != nil {
		return fmt.Errorf("could not read ca certificate: %s", err)
	}

	if ok := certPool.AppendCertsFromPEM(ca); !ok {
		return errors.New("failed to append client certs")
	}

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("could not list on %s: %s", addr, err)
	}

	creds := credentials.NewTLS(&tls.Config{
		ClientAuth:   tls.RequireAndVerifyClientCert,
		Certificates: []tls.Certificate{certificate},
		ClientCAs:    certPool,
	})

	srv := grpc.NewServer(grpc.Creds(creds))

	server := &fneticServer{
		gRPCServer: srv,
	}

	broker.RegisterBrokerServer(srv, server)

	log.Println("Listening on ", addr)

	if err := srv.Serve(lis); err != nil {
		return fmt.Errorf("grpc serve error: %s", err)
	}
	return nil
}

func main() {
	err := startServer("localhost:9190")
	if err != nil {
		log.Fatal(err)
	}
}
