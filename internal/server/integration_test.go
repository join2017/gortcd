package server

import (
	"bytes"
	"fmt"
	"net"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/gortc/gortcd/internal/auth"
	"github.com/gortc/gortcd/internal/testutil"
	"github.com/gortc/turn"
)

func TestServerIntegration(t *testing.T) {
	// Test is same as e2e/gortc-turn.
	const (
		username = "username"
		password = "password"
		realm    = "realm"
	)
	echoConn, echoUDPAddr := listenUDP(t)
	serverConn, serverUDPAddr := listenUDP(t)
	serverCore, serverLogs := observer.New(zap.DebugLevel)
	defer testutil.EnsureNoErrors(t, serverLogs)
	s, err := New(Options{
		Log:   zap.New(serverCore),
		Conn:  serverConn,
		Realm: realm,
		Auth: auth.NewStatic([]auth.StaticCredential{
			{Username: username, Password: password, Realm: realm},
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	}()
	go func() {
		for {
			buf := make([]byte, 1024)
			n, addr, err := echoConn.ReadFromUDP(buf)
			if err != nil {
				t.Errorf("peer: failed to read: %v", err)
			}
			t.Logf("peer: got message: %s", string(buf[:n]))
			if _, err := echoConn.WriteToUDP(buf[:n], addr); err != nil {
				t.Errorf("peer: failed to write back: %v", err)
			}
			t.Logf("peer: echoed back")
		}
	}()
	go func() {
		if err := s.Serve(); err != nil {
			t.Error(err)
		}
	}()
	// Creating connection from client to server.
	c, err := net.DialUDP("udp", nil, serverUDPAddr)
	if err != nil {
		t.Fatalf("failed to dial to TURN server: %v", err)
	}
	clientCore, clientLogs := observer.New(zap.DebugLevel)
	defer testutil.EnsureNoErrors(t, clientLogs)
	client, err := turn.NewClient(turn.ClientOptions{
		Log:      zap.New(clientCore),
		Conn:     c,
		Username: username,
		Password: password,
	})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	a, err := client.Allocate()
	if err != nil {
		t.Fatalf("failed to create allocation: %v", err)
	}
	p, err := a.Create(echoUDPAddr)
	if err != nil {
		t.Fatalf("failed to create permission: %v", err)
	}
	// Sending and receiving "hello" message.
	if _, err := fmt.Fprint(p, "hello"); err != nil {
		t.Fatal("failed to write data")
	}
	sent := []byte("hello")
	got := make([]byte, len(sent))
	if _, err = p.Read(got); err != nil {
		t.Fatalf("failed to read data: %v", err)
	}
	if !bytes.Equal(got, sent) {
		t.Fatal("got incorrect data")
	}
	// Repeating via channel binding.
	for i := range got {
		got[i] = 0
	}
	if bindErr := p.Bind(); bindErr != nil {
		t.Fatal("failed to bind", zap.Error(err))
	}
	if !p.Bound() {
		t.Fatal("should be bound")
	}
	t.Logf("bound to channel: 0x%x", int(p.Binding()))
	if _, err := fmt.Fprint(p, "hello"); err != nil {
		t.Fatalf("failed to write data: %v", err)
	}
	if _, err = p.Read(got); err != nil {
		t.Fatalf("failed to read data: %v", err)
	}
	t.Logf("client: got message: %s", string(got))
	if !bytes.Equal(got, sent) {
		t.Error("data mismatch")
	}
}
