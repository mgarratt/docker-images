// proton-bridge-starttls-relay is a small, stateless STARTTLS-terminating
// relay for SMTP and IMAP. It sits in front of one of Bridge's loopback ports
// and replaces Bridge's own certificate -- correct only for a same-machine
// client (CN=127.0.0.1, SAN IP:127.0.0.1) -- with one this cluster's clients
// can actually verify by name.
//
// Usage: proton-bridge-starttls-relay smtp|imap
//
// For protocol P (SMTP or IMAP) it listens on CONTAINER_P_PORT, offers
// STARTTLS to inbound clients using a certificate/key mounted at
// CONTAINER_P_TLS_CERT_FILE and CONTAINER_P_TLS_KEY_FILE, and negotiates
// STARTTLS onward to Bridge at PROTON_BRIDGE_HOST:PROTON_BRIDGE_P_PORT --
// exactly as today's clients do, so no assumption is made about Bridge
// accepting plaintext. The inner hop skips certificate verification: it is
// loopback inside one pod, to a certificate whose IP SAN genuinely matches.
//
// Once both legs are TLS, the relay pipes bytes verbatim in both directions.
// It never parses AUTH/LOGIN, MAIL, RCPT, DATA or any IMAP command after
// STARTTLS, so credentials pass through exactly as Bridge issued them -- the
// relay authenticates nothing of its own.
//
// Set P_RELAY_DEBUG=true (SMTP_RELAY_DEBUG, IMAP_RELAY_DEBUG) for verbose
// per-connection logging.
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

// negotiationTimeout bounds the plaintext dialogue on each leg, before its
// TLS handshake completes. Once a leg is TLS, its deadline is cleared and the
// pipe runs for the life of the connection.
const negotiationTimeout = 30 * time.Second

// protocol holds the plaintext STARTTLS dialogue for one mail protocol.
type protocol struct {
	// inbound speaks to the client until it issues STARTTLS, and reports
	// whether it did (false: the client quit or broke off).
	inbound func(conn net.Conn, r *bufio.Reader) bool
	// outbound drives Bridge through its STARTTLS dialogue.
	outbound func(conn net.Conn, r *bufio.Reader) error
}

var protocols = map[string]protocol{
	"SMTP": {inbound: smtpInbound, outbound: smtpOutbound},
	"IMAP": {inbound: imapInbound, outbound: imapOutbound},
}

func main() {
	if len(os.Args) != 2 {
		log.Fatalf("usage: %s smtp|imap", os.Args[0])
	}
	name := strings.ToUpper(os.Args[1])
	proto, ok := protocols[name]
	if !ok {
		log.Fatalf("unknown protocol %q, want smtp or imap", os.Args[1])
	}

	listenPort := envOrFatal("CONTAINER_" + name + "_PORT")
	bridgeAddr := net.JoinHostPort(envOrFatal("PROTON_BRIDGE_HOST"), envOrFatal("PROTON_BRIDGE_"+name+"_PORT"))
	certFile := envOrFatal("CONTAINER_" + name + "_TLS_CERT_FILE")
	keyFile := envOrFatal("CONTAINER_" + name + "_TLS_KEY_FILE")
	debug := strings.EqualFold(os.Getenv(name+"_RELAY_DEBUG"), "true")

	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		log.Fatalf("load TLS keypair (%s, %s): %v", certFile, keyFile, err)
	}
	tlsConfig := &tls.Config{Certificates: []tls.Certificate{cert}}

	ln, err := net.Listen("tcp", ":"+listenPort)
	if err != nil {
		log.Fatalf("listen on :%s: %v", listenPort, err)
	}
	log.Printf("proton-bridge-starttls-relay (%s) listening on :%s, relaying to %s", name, listenPort, bridgeAddr)

	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("accept: %v", err)
			continue
		}
		go handle(conn, proto, bridgeAddr, tlsConfig, debug)
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
func handle(client net.Conn, proto protocol, bridgeAddr string, tlsConfig *tls.Config, debug bool) {
	defer client.Close()
	remote := client.RemoteAddr()
	if debug {
		log.Printf("[%s] accepted", remote)
	}

	clientTLS, err := negotiateInbound(client, proto, tlsConfig)
	if err != nil {
		if debug {
			log.Printf("[%s] inbound: %v", remote, err)
		}
		return
	}
	defer clientTLS.Close()

	bridgeTLS, err := negotiateOutbound(bridgeAddr, proto)
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

// negotiateInbound runs the protocol's plaintext dialogue up to STARTTLS, then
// performs the TLS handshake as the server and returns the upgraded connection.
func negotiateInbound(conn net.Conn, proto protocol, tlsConfig *tls.Config) (*tls.Conn, error) {
	_ = conn.SetDeadline(time.Now().Add(negotiationTimeout))
	if !proto.inbound(conn, bufio.NewReader(conn)) {
		return nil, fmt.Errorf("client did not issue STARTTLS")
	}
	tlsConn := tls.Server(conn, tlsConfig)
	if err := tlsConn.Handshake(); err != nil {
		return nil, fmt.Errorf("TLS handshake with client: %w", err)
	}
	_ = tlsConn.SetDeadline(time.Time{})
	return tlsConn, nil
}

// negotiateOutbound dials Bridge and performs the same STARTTLS dialogue a
// client performs today, so no assumption is made about Bridge accepting
// plaintext. Verification is skipped: this hop is loopback inside one pod, to
// a certificate whose IP SAN genuinely matches.
func negotiateOutbound(addr string, proto protocol) (*tls.Conn, error) {
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
	if err := proto.outbound(conn, bufio.NewReader(conn)); err != nil {
		return nil, err
	}

	tlsConn := tls.Client(conn, &tls.Config{InsecureSkipVerify: true}) //nolint:gosec // loopback hop to Bridge's own IP-SAN cert, see package doc
	if err := tlsConn.Handshake(); err != nil {
		return nil, fmt.Errorf("TLS handshake with bridge: %w", err)
	}
	_ = tlsConn.SetDeadline(time.Time{})
	ok = true
	return tlsConn, nil
}

// smtpInbound speaks just enough SMTP to reach STARTTLS: it advertises
// STARTTLS on EHLO and refuses anything that would put credentials or mail on
// the wire in the clear.
func smtpInbound(conn net.Conn, r *bufio.Reader) bool {
	sendLine(conn, "220 smtp-relay ESMTP ready")
	for {
		line, err := readLine(r)
		if err != nil {
			return false
		}
		switch commandWord(line) {
		case "EHLO":
			sendLines(conn, "250-smtp-relay, at your service", "250 STARTTLS")
		case "HELO":
			sendLine(conn, "250 smtp-relay, at your service")
		case "STARTTLS":
			sendLine(conn, "220 2.0.0 ready to start TLS")
			return true
		case "NOOP":
			sendLine(conn, "250 2.0.0 OK")
		case "QUIT":
			sendLine(conn, "221 2.0.0 Bye")
			return false
		default:
			// AUTH, MAIL, RCPT, DATA and anything else are refused rather than
			// silently downgraded: this relay's whole point is that mail and
			// credentials never travel outside a TLS tunnel it terminates.
			sendLine(conn, "530 5.7.0 Must issue a STARTTLS command first")
		}
	}
}

// smtpOutbound: EHLO, STARTTLS.
func smtpOutbound(conn net.Conn, r *bufio.Reader) error {
	if _, err := readReply(r); err != nil {
		return fmt.Errorf("read banner: %w", err)
	}
	sendLine(conn, "EHLO smtp-relay")
	if _, err := readReply(r); err != nil {
		return fmt.Errorf("read EHLO reply: %w", err)
	}
	sendLine(conn, "STARTTLS")
	if _, err := readReply(r); err != nil {
		return fmt.Errorf("read STARTTLS reply: %w", err)
	}
	return nil
}

// imapCapability is what the relay advertises before TLS. LOGINDISABLED tells
// clients not to send credentials in the clear; after STARTTLS clients must
// discard it and re-issue CAPABILITY, which then reaches Bridge.
const imapCapability = "CAPABILITY IMAP4rev1 STARTTLS LOGINDISABLED"

// imapInbound speaks just enough IMAP to reach STARTTLS (RFC 9051 6.2.1). No
// greeting is sent after the handshake, matching IMAP STARTTLS semantics and
// Bridge's own behaviour, so the two TLS legs line up when piped.
func imapInbound(conn net.Conn, r *bufio.Reader) bool {
	sendLine(conn, "* OK ["+imapCapability+"] imap-relay ready")
	for {
		line, err := readLine(r)
		if err != nil {
			return false
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		tag := fields[0]
		if len(fields) < 2 {
			sendLine(conn, "* BAD missing command")
			continue
		}
		switch strings.ToUpper(fields[1]) {
		case "CAPABILITY":
			sendLines(conn, "* "+imapCapability, tag+" OK CAPABILITY completed")
		case "STARTTLS":
			sendLine(conn, tag+" OK begin TLS negotiation now")
			return true
		case "NOOP":
			sendLine(conn, tag+" OK NOOP completed")
		case "LOGOUT":
			sendLines(conn, "* BYE imap-relay logging out", tag+" OK LOGOUT completed")
			return false
		case "LOGIN", "AUTHENTICATE":
			sendLine(conn, tag+" NO [PRIVACYREQUIRED] issue STARTTLS first")
		default:
			sendLine(conn, tag+" BAD issue STARTTLS first")
		}
	}
}

// imapOutbound: read the greeting, then STARTTLS and require a tagged OK.
func imapOutbound(conn net.Conn, r *bufio.Reader) error {
	if _, err := readLine(r); err != nil {
		return fmt.Errorf("read greeting: %w", err)
	}
	sendLine(conn, "R1 STARTTLS")
	for {
		line, err := readLine(r)
		if err != nil {
			return fmt.Errorf("read STARTTLS reply: %w", err)
		}
		if !strings.HasPrefix(line, "R1 ") {
			continue // untagged response
		}
		if !strings.HasPrefix(strings.ToUpper(line), "R1 OK") {
			return fmt.Errorf("bridge refused STARTTLS: %s", line)
		}
		return nil
	}
}

// pipe copies bytes in both directions until either side is done, then
// returns; the caller's deferred Close calls on both connections unblock the
// other copy. Everything after STARTTLS crosses here unexamined.
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
