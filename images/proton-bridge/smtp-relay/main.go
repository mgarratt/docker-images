// proton-bridge-smtp-relay is a small, stateless STARTTLS-terminating SMTP
// relay. It sits in front of Bridge's loopback SMTP port and replaces
// Bridge's own certificate -- correct only for a same-machine client
// (CN=127.0.0.1, SAN IP:127.0.0.1) -- with one this cluster's clients can
// actually verify by name.
//
// It listens on CONTAINER_SMTP_PORT, offers STARTTLS to inbound clients using
// a certificate/key mounted at CONTAINER_SMTP_TLS_CERT_FILE and
// CONTAINER_SMTP_TLS_KEY_FILE, and negotiates STARTTLS onward to Bridge at
// PROTON_BRIDGE_HOST:PROTON_BRIDGE_SMTP_PORT -- exactly as today's clients do,
// so no assumption is made about Bridge accepting plaintext. The inner hop
// skips certificate verification: it is loopback inside one pod, to a
// certificate whose IP SAN genuinely matches.
//
// Once both legs are TLS, the relay pipes bytes verbatim in both directions.
// It never parses AUTH, MAIL, RCPT or DATA, so SMTP AUTH passes through
// exactly as Bridge issued it -- the relay authenticates nothing of its own.
//
// Set SMTP_RELAY_DEBUG=true for verbose per-connection logging.
package main

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strings"
	"time"
)

// negotiationTimeout bounds the plaintext SMTP dialogue on each leg, before
// its TLS handshake completes. Once a leg is TLS, its deadline is cleared and
// the pipe runs for the life of the connection.
const negotiationTimeout = 30 * time.Second

func main() {
	listenPort := envOrFatal("CONTAINER_SMTP_PORT")
	bridgeAddr := net.JoinHostPort(envOrFatal("PROTON_BRIDGE_HOST"), envOrFatal("PROTON_BRIDGE_SMTP_PORT"))
	certFile := envOrFatal("CONTAINER_SMTP_TLS_CERT_FILE")
	keyFile := envOrFatal("CONTAINER_SMTP_TLS_KEY_FILE")
	debug := strings.EqualFold(os.Getenv("SMTP_RELAY_DEBUG"), "true")

	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		log.Fatalf("load TLS keypair (%s, %s): %v", certFile, keyFile, err)
	}
	tlsConfig := &tls.Config{Certificates: []tls.Certificate{cert}}

	ln, err := net.Listen("tcp", ":"+listenPort)
	if err != nil {
		log.Fatalf("listen on :%s: %v", listenPort, err)
	}
	log.Printf("proton-bridge-smtp-relay listening on :%s, relaying to %s", listenPort, bridgeAddr)

	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("accept: %v", err)
			continue
		}
		go handle(conn, bridgeAddr, tlsConfig, debug)
	}
}

func envOrFatal(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("required environment variable %s is not set", key)
	}
	return v
}

// handle drives one client connection: negotiate STARTTLS inbound, negotiate
// STARTTLS outbound to Bridge, then pipe the two TLS legs together.
func handle(client net.Conn, bridgeAddr string, tlsConfig *tls.Config, debug bool) {
	defer client.Close()
	remote := client.RemoteAddr()
	if debug {
		log.Printf("[%s] accepted", remote)
	}

	clientTLS, ok := negotiateInbound(client, tlsConfig, debug)
	if !ok {
		return
	}
	defer clientTLS.Close()

	bridgeTLS, err := negotiateOutbound(bridgeAddr)
	if err != nil {
		log.Printf("[%s] connect to bridge %s: %v", remote, bridgeAddr, err)
		return
	}
	defer bridgeTLS.Close()

	if debug {
		log.Printf("[%s] both legs are TLS, piping", remote)
	}
	pipe(clientTLS, bridgeTLS)
	if debug {
		log.Printf("[%s] closed", remote)
	}
}

// negotiateInbound speaks just enough SMTP to reach STARTTLS: it advertises
// STARTTLS on EHLO and refuses anything that would put credentials or mail on
// the wire in the clear. Once the client issues STARTTLS it performs the TLS
// handshake as the server and returns the upgraded connection.
func negotiateInbound(conn net.Conn, tlsConfig *tls.Config, debug bool) (*tls.Conn, bool) {
	_ = conn.SetDeadline(time.Now().Add(negotiationTimeout))
	r := bufio.NewReader(conn)

	sendLine(conn, "220 smtp-relay ESMTP ready")

	for {
		line, err := readLine(r)
		if err != nil {
			return nil, false
		}
		switch commandWord(line) {
		case "EHLO":
			sendLines(conn, "250-smtp-relay, at your service", "250 STARTTLS")
		case "HELO":
			sendLine(conn, "250 smtp-relay, at your service")
		case "STARTTLS":
			sendLine(conn, "220 2.0.0 ready to start TLS")
			tlsConn := tls.Server(conn, tlsConfig)
			if err := tlsConn.Handshake(); err != nil {
				if debug {
					log.Printf("TLS handshake with client failed: %v", err)
				}
				return nil, false
			}
			_ = tlsConn.SetDeadline(time.Time{})
			return tlsConn, true
		case "NOOP":
			sendLine(conn, "250 2.0.0 OK")
		case "QUIT":
			sendLine(conn, "221 2.0.0 Bye")
			return nil, false
		default:
			// AUTH, MAIL, RCPT, DATA and anything else are refused rather than
			// silently downgraded: this relay's whole point is that mail and
			// credentials never travel outside a TLS tunnel it terminates.
			sendLine(conn, "530 5.7.0 Must issue a STARTTLS command first")
		}
	}
}

// negotiateOutbound dials Bridge and performs the same STARTTLS dance a
// client performs today (EHLO, STARTTLS, TLS handshake), so no assumption is
// made about Bridge accepting plaintext. Verification is skipped: this hop is
// loopback inside one pod, to a certificate whose IP SAN genuinely matches.
func negotiateOutbound(addr string) (*tls.Conn, error) {
	conn, err := net.DialTimeout("tcp", addr, negotiationTimeout)
	if err != nil {
		return nil, err
	}
	ok := false
	defer func() {
		if !ok {
			conn.Close()
		}
	}()

	_ = conn.SetDeadline(time.Now().Add(negotiationTimeout))
	r := bufio.NewReader(conn)

	if _, err := readReply(r); err != nil {
		return nil, fmt.Errorf("read banner: %w", err)
	}
	sendLine(conn, "EHLO smtp-relay")
	if _, err := readReply(r); err != nil {
		return nil, fmt.Errorf("read EHLO reply: %w", err)
	}
	sendLine(conn, "STARTTLS")
	if _, err := readReply(r); err != nil {
		return nil, fmt.Errorf("read STARTTLS reply: %w", err)
	}

	tlsConn := tls.Client(conn, &tls.Config{InsecureSkipVerify: true}) //nolint:gosec // loopback hop to Bridge's own IP-SAN cert, see package doc
	if err := tlsConn.Handshake(); err != nil {
		return nil, fmt.Errorf("TLS handshake with bridge: %w", err)
	}
	_ = tlsConn.SetDeadline(time.Time{})
	ok = true
	return tlsConn, nil
}

// pipe copies bytes in both directions until either side is done, then
// returns; the caller's deferred Close calls on both connections unblock the
// other copy. AUTH, MAIL, RCPT and DATA all cross here unexamined.
func pipe(a, b io.ReadWriter) {
	done := make(chan struct{}, 2)
	cp := func(dst io.Writer, src io.Reader) {
		_, _ = io.Copy(dst, src)
		done <- struct{}{}
	}
	go cp(a, b)
	go cp(b, a)
	<-done
}

func readLine(r *bufio.Reader) (string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// readReply reads one (possibly multi-line) SMTP reply, e.g. "250-a\r\n250
// b\r\n", and returns its final line.
func readReply(r *bufio.Reader) (string, error) {
	var last string
	for {
		line, err := readLine(r)
		if err != nil {
			return "", err
		}
		last = line
		if len(line) < 4 || line[3] != '-' {
			return last, nil
		}
	}
}

func commandWord(line string) string {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return ""
	}
	return strings.ToUpper(fields[0])
}

func sendLine(w io.Writer, line string) {
	_, _ = io.WriteString(w, line+"\r\n")
}

func sendLines(w io.Writer, lines ...string) {
	for _, l := range lines {
		sendLine(w, l)
	}
}
