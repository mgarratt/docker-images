// proton-bridge-exporter scrapes the headless Proton Bridge gRPC frontend
// (started with `bridge --grpc`) and exposes Prometheus metrics about the
// bridge process and, crucially, per-account login/sync state so that dropped
// authentication can be alerted on.
//
// It connects to the gRPC service the same way the desktop GUI does: by reading
// the TLS cert, auth token and unix socket path that bridge writes to
// grpcServerConfig.json, then pinning that self-signed cert (CN 127.0.0.1) and
// presenting the token in a per-RPC "server-token" metadata header.
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/protobuf/types/known/emptypb"

	pb "proton-bridge-exporter/bridgepb"
)

const serverTokenMetadataKey = "server-token"

type serverConfig struct {
	Port           int    `json:"port"`
	Cert           string `json:"cert"`
	Token          string `json:"token"`
	FileSocketPath string `json:"fileSocketPath"`
}

var (
	up = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "proton_bridge_up",
		Help: "Whether the Proton Bridge gRPC service is reachable (1) or not (0).",
	})
	info = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "proton_bridge_info",
		Help: "Proton Bridge version, value is always 1.",
	}, []string{"version"})
	accountsTotal = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "proton_bridge_accounts_total",
		Help: "Number of accounts known to the bridge.",
	})
	accountConnected = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "proton_bridge_account_connected",
		Help: "Whether an account is connected/logged in (1) or not (0).",
	}, []string{"account"})
	accountState = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "proton_bridge_account_state",
		Help: "Current account state, value 1 for the active state label.",
	}, []string{"account", "state"})
	accountUsedBytes = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "proton_bridge_account_used_bytes",
		Help: "Mail storage used by the account in bytes.",
	}, []string{"account"})
	accountTotalBytes = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "proton_bridge_account_total_bytes",
		Help: "Mail storage quota for the account in bytes.",
	}, []string{"account"})
	internetConnected = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "proton_bridge_internet_connected",
		Help: "Whether bridge reports internet connectivity (1) or not (0).",
	})
	userDisconnectedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "proton_bridge_user_disconnected_total",
		Help: "Number of account disconnection events observed (dropped auth).",
	}, []string{"account"})
	userBadEventTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "proton_bridge_user_bad_event_total",
		Help: "Number of account bad-event (sync error) events observed.",
	}, []string{"account"})
	imapLoginFailedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "proton_bridge_imap_login_failed_total",
		Help: "Number of failed IMAP client logins against the bridge.",
	}, []string{"account"})
	scrapeErrorsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "proton_bridge_exporter_scrape_errors_total",
		Help: "Number of errors polling the bridge gRPC service.",
	})
)

// tokenAuth presents the server token as per-RPC metadata over the TLS channel.
type tokenAuth struct{ token string }

func (t tokenAuth) GetRequestMetadata(context.Context, ...string) (map[string]string, error) {
	return map[string]string{serverTokenMetadataKey: t.token}, nil
}
func (t tokenAuth) RequireTransportSecurity() bool { return true }

func loadConfig(path string) (serverConfig, error) {
	var cfg serverConfig
	raw, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return cfg, err
	}
	if cfg.FileSocketPath == "" {
		return cfg, errors.New("grpcServerConfig.json has no fileSocketPath (expected a unix socket on linux)")
	}
	return cfg, nil
}

func dial(cfg serverConfig) (*grpc.ClientConn, error) {
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM([]byte(cfg.Cert)) {
		return nil, errors.New("failed to parse bridge TLS certificate")
	}
	creds := credentials.NewTLS(&tls.Config{RootCAs: pool, ServerName: "127.0.0.1"})
	socket := cfg.FileSocketPath
	return grpc.NewClient(
		"passthrough:///"+socket,
		grpc.WithTransportCredentials(creds),
		grpc.WithPerRPCCredentials(tokenAuth{token: cfg.Token}),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socket)
		}),
	)
}

func poll(ctx context.Context, client pb.BridgeClient) error {
	resp, err := client.GetUserList(ctx, &emptypb.Empty{})
	if err != nil {
		return err
	}
	accountConnected.Reset()
	accountState.Reset()
	accountUsedBytes.Reset()
	accountTotalBytes.Reset()

	users := resp.GetUsers()
	accountsTotal.Set(float64(len(users)))
	for _, u := range users {
		name := u.GetUsername()
		state := u.GetState()
		connected := 0.0
		if state == pb.UserState_CONNECTED {
			connected = 1
		}
		accountConnected.WithLabelValues(name).Set(connected)
		for _, s := range []pb.UserState{pb.UserState_SIGNED_OUT, pb.UserState_LOCKED, pb.UserState_CONNECTED} {
			v := 0.0
			if s == state {
				v = 1
			}
			accountState.WithLabelValues(name, strings.ToLower(s.String())).Set(v)
		}
		accountUsedBytes.WithLabelValues(name).Set(float64(u.GetUsedBytes()))
		accountTotalBytes.WithLabelValues(name).Set(float64(u.GetTotalBytes()))
	}

	if v, err := client.Version(ctx, &emptypb.Empty{}); err == nil {
		info.Reset()
		info.WithLabelValues(v.GetValue()).Set(1)
	}
	return nil
}

// streamEvents subscribes to the bridge event stream and translates the events
// we care about into counters/gauges. Returns on stream error so the caller can
// reconnect.
func streamEvents(ctx context.Context, client pb.BridgeClient) error {
	stream, err := client.RunEventStream(ctx, &pb.EventStreamRequest{ClientPlatform: "exporter"})
	if err != nil {
		return err
	}
	for {
		ev, err := stream.Recv()
		if err != nil {
			return err
		}
		if app := ev.GetApp(); app != nil {
			if is := app.GetInternetStatus(); is != nil {
				v := 0.0
				if is.GetConnected() {
					v = 1
				}
				internetConnected.Set(v)
			}
		}
		if ue := ev.GetUser(); ue != nil {
			switch {
			case ue.GetUserDisconnected() != nil:
				userDisconnectedTotal.WithLabelValues(ue.GetUserDisconnected().GetUsername()).Inc()
			case ue.GetUserBadEvent() != nil:
				userBadEventTotal.WithLabelValues(ue.GetUserBadEvent().GetUserID()).Inc()
			case ue.GetImapLoginFailedEvent() != nil:
				imapLoginFailedTotal.WithLabelValues(ue.GetImapLoginFailedEvent().GetUsername()).Inc()
			}
		}
	}
}

func run(ctx context.Context, configPath string, pollInterval time.Duration) {
	for ctx.Err() == nil {
		cfg, err := loadConfig(configPath)
		if err != nil {
			up.Set(0)
			log.Printf("waiting for bridge gRPC config %s: %v", configPath, err)
			sleep(ctx, pollInterval)
			continue
		}
		conn, err := dial(cfg)
		if err != nil {
			up.Set(0)
			scrapeErrorsTotal.Inc()
			log.Printf("dial bridge: %v", err)
			sleep(ctx, pollInterval)
			continue
		}
		client := pb.NewBridgeClient(conn)

		// Event stream runs until it errors (e.g. bridge restart); we then
		// fall through, close the conn and reconnect.
		streamCtx, cancel := context.WithCancel(ctx)
		streamErr := make(chan error, 1)
		go func() { streamErr <- streamEvents(streamCtx, client) }()

		for ctx.Err() == nil {
			if err := poll(ctx, client); err != nil {
				up.Set(0)
				scrapeErrorsTotal.Inc()
				log.Printf("poll bridge: %v", err)
				break
			}
			up.Set(1)
			select {
			case err := <-streamErr:
				log.Printf("event stream ended: %v", err)
				goto reconnect
			case <-time.After(pollInterval):
			case <-ctx.Done():
			}
		}
	reconnect:
		cancel()
		_ = conn.Close()
		sleep(ctx, pollInterval)
	}
}

func sleep(ctx context.Context, d time.Duration) {
	select {
	case <-time.After(d):
	case <-ctx.Done():
	}
}

func defaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "/home/bridge"
	}
	return filepath.Join(home, ".config", "protonmail", "bridge-v3", "grpcServerConfig.json")
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	configPath := envOr("BRIDGE_GRPC_CONFIG", defaultConfigPath())
	listen := ":" + envOr("CONTAINER_METRICS_PORT", "9154")

	ctx := context.Background()
	go run(ctx, configPath, 15*time.Second)

	http.Handle("/metrics", promhttp.Handler())
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/metrics", http.StatusFound)
	})
	log.Printf("proton-bridge-exporter listening on %s, config %s", listen, configPath)
	srv := &http.Server{Addr: listen, ReadHeaderTimeout: 5 * time.Second}
	log.Fatal(srv.ListenAndServe())
}
