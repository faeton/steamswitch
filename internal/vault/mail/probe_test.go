package mail

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"net"
	"reflect"
	"testing"
	"time"

	"github.com/emersion/go-imap/backend/memory"
	"github.com/emersion/go-imap/server"
)

// startIMAP runs a real IMAP server over TLS. The memory backend has one user,
// username/password, with one message in INBOX.
func startIMAP(t *testing.T) (host string, port int, roots *x509.CertPool) {
	t.Helper()
	roots, cfg := testTLS(t)
	ln, err := tls.Listen("tcp", "127.0.0.1:0", cfg)
	if err != nil {
		t.Fatal(err)
	}
	srv := server.New(memory.New())
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return "localhost", ln.Addr().(*net.TCPAddr).Port, roots
}

func testTLS(t *testing.T) (*x509.CertPool, *tls.Config) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "localhost"},
		DNSNames:              []string{"localhost"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(cert)
	return roots, &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}
}

func TestIMAPProbe_CountsTheInbox(t *testing.T) {
	host, port, roots := startIMAP(t)
	src, err := NewIMAP(IMAPConfig{Host: host, Port: port, User: "username", Password: "password", UseTLS: true, rootCAs: roots})
	if err != nil {
		t.Fatal(err)
	}
	n, err := src.Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if n != 1 {
		t.Fatalf("Probe counted %d messages, want 1", n)
	}
}

func TestIMAPProbe_WrongPasswordIsAuthNotConnect(t *testing.T) {
	host, port, roots := startIMAP(t)
	src, err := NewIMAP(IMAPConfig{Host: host, Port: port, User: "username", Password: "nope", UseTLS: true, rootCAs: roots})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := src.Probe(context.Background()); !errors.Is(err, ErrAuth) {
		t.Fatalf("Probe = %v, want ErrAuth", err)
	}
}

// The certificate is checked for real: without the test root the server must be refused, and
// refused before any password is sent.
func TestIMAPProbe_UntrustedCertificateIsRefused(t *testing.T) {
	host, port, _ := startIMAP(t)
	src, err := NewIMAP(IMAPConfig{Host: host, Port: port, User: "username", Password: "password", UseTLS: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := src.Probe(context.Background()); !errors.Is(err, ErrConnect) {
		t.Fatalf("Probe = %v, want ErrConnect", err)
	}
}

// A server that greets and then hangs up during LOGIN has not rejected anything.
func TestIMAPProbe_DroppedDuringLoginIsConnectNotAuth(t *testing.T) {
	roots, cfg := testTLS(t)
	ln, err := tls.Listen("tcp", "127.0.0.1:0", cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = conn.Close() }()
				_, _ = conn.Write([]byte("* OK ready\r\n"))
				buf := make([]byte, 512)
				_, _ = conn.Read(buf) // the LOGIN command, then hang up without answering
			}()
		}
	}()
	src, err := NewIMAP(IMAPConfig{Host: "localhost", Port: ln.Addr().(*net.TCPAddr).Port, User: "u", Password: "p", UseTLS: true, rootCAs: roots})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := src.Probe(context.Background()); !errors.Is(err, ErrConnect) {
		t.Fatalf("Probe = %v, want ErrConnect", err)
	}
}

func TestNewIMAP_Port993IsAlwaysTLS(t *testing.T) {
	src, err := NewIMAP(IMAPConfig{Host: "h", Port: 993, User: "u", Password: "p", UseTLS: false})
	if err != nil {
		t.Fatal(err)
	}
	if !src.(*imapSource).cfg.UseTLS {
		t.Fatal("port 993 was left as plaintext")
	}
}

func TestKnownProvider(t *testing.T) {
	cases := []struct {
		address, name, access string
	}{
		{"a@gmail.com", "Gmail", AccessAppPassword},
		{"a@GMAIL.com", "Gmail", AccessAppPassword},
		{"a@hotmail.co.uk", "Outlook", AccessNoIMAP},
		{"a@pm.me", "Proton Mail", AccessNoIMAP},
		{"a@firstmail.ltd", "", ""},
		{"a@zorrodemail.com", "", ""},
		{"not-an-address", "", ""},
	}
	for _, c := range cases {
		p, _ := KnownProvider(c.address)
		if p.Name != c.name || p.Access != c.access {
			t.Errorf("KnownProvider(%q) = %+v, want %s/%s", c.address, p, c.name, c.access)
		}
	}
}

func TestDetect_KnownProviderNeedsNoNetwork(t *testing.T) {
	got := Detect(context.Background(), "a@gmail.com")
	if !got.Found || got.Host != "imap.gmail.com" || got.Access != AccessAppPassword {
		t.Fatalf("Detect = %+v", got)
	}
	if got := Detect(context.Background(), "a@outlook.com"); got.Found || got.Access != AccessNoIMAP {
		t.Fatalf("Detect = %+v", got)
	}
}

func TestMXCandidates(t *testing.T) {
	lookup := func(context.Context, string) ([]*net.MX, error) {
		return []*net.MX{{Host: "MX.firstmail.ltd."}, {Host: "bare.example."}, {Host: "third.example.net."}}, nil
	}
	got := mxCandidates(context.Background(), "vanity.test", lookup)
	want := []string{"imap.firstmail.ltd", "mx.firstmail.ltd", "bare.example"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mxCandidates = %v, want %v", got, want)
	}
	failing := func(context.Context, string) ([]*net.MX, error) { return nil, errors.New("no") }
	if got := mxCandidates(context.Background(), "vanity.test", failing); got != nil {
		t.Fatalf("mxCandidates on a lookup failure = %v, want nil", got)
	}
}
