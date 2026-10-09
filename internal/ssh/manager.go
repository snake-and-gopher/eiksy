package sshmanager

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"eiksy/internal/sshauth"

	xssh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// ErrUnknownHostKey is the sentinel prefix used when a host key is not in known_hosts.
const ErrUnknownHostKey = "unknown host key"

// PendingHostKey holds the key info captured when a host is not in known_hosts.
type PendingHostKey struct {
	Hostname    string
	Remote      net.Addr
	Key         xssh.PublicKey
	Fingerprint string
	KeyType     string
}

type Manager struct {
	mu          sync.RWMutex
	connections map[string]*connection
	handlers    map[string]func(string)
	pendingKeys map[string]*PendingHostKey // keyed by tabID
}

type connection struct {
	client         *xssh.Client
	session        *xssh.Session
	stdin          io.WriteCloser
	handler        func(string)
	localListeners []net.Listener
}

func NewManager() *Manager {
	return &Manager{
		connections: map[string]*connection{},
		handlers:    map[string]func(string){},
		pendingKeys: map[string]*PendingHostKey{},
	}
}

func (m *Manager) Connect(ctx context.Context, tabID, host string, port int, user, password string, options map[string]string) error {
	if port <= 0 {
		port = 22
	}
	// Connect replaces the existing transport, but the output handler belongs to
	// the UI terminal and must survive that replacement. Disconnect intentionally
	// clears per-connection state, so preserve and restore the handler around it.
	m.mu.RLock()
	outputHandler := m.handlers[tabID]
	m.mu.RUnlock()
	_ = m.Disconnect(tabID)
	if outputHandler != nil {
		m.SetOutputHandler(tabID, outputHandler)
	}

	hostKey, err := m.hostKeyCallback(tabID)
	if err != nil {
		return err
	}
	config, err := buildClientConfig(user, password, options, hostKey)
	if err != nil {
		return err
	}
	address := net.JoinHostPort(host, strconv.Itoa(port))
	netConn, err := dialTarget(ctx, address, config, options)
	if err != nil {
		return err
	}

	conn, chans, reqs, err := xssh.NewClientConn(netConn, address, config)
	if err != nil {
		_ = netConn.Close()
		return fmt.Errorf("establish ssh session: %w", err)
	}
	client := xssh.NewClient(conn, chans, reqs)
	session, err := client.NewSession()
	if err != nil {
		_ = client.Close()
		return fmt.Errorf("create ssh session: %w", err)
	}
	stdin, err := session.StdinPipe()
	if err != nil {
		_ = session.Close()
		_ = client.Close()
		return fmt.Errorf("open ssh stdin: %w", err)
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		_ = session.Close()
		_ = client.Close()
		return fmt.Errorf("open ssh stdout: %w", err)
	}

	modes := xssh.TerminalModes{xssh.ECHO: 1, xssh.TTY_OP_ISPEED: 14400, xssh.TTY_OP_OSPEED: 14400}
	if err := session.RequestPty("xterm-256color", 24, 80, modes); err != nil {
		_ = session.Close()
		_ = client.Close()
		return fmt.Errorf("request ssh pty: %w", err)
	}
	if err := session.Shell(); err != nil {
		_ = session.Close()
		_ = client.Close()
		return fmt.Errorf("start ssh shell: %w", err)
	}

	listeners, err := startLocalForwards(client, options)
	if err != nil {
		_ = session.Close()
		_ = client.Close()
		return err
	}

	m.mu.Lock()
	handler := m.handlers[tabID]
	connState := &connection{client: client, session: session, stdin: stdin, handler: handler, localListeners: listeners}
	m.connections[tabID] = connState
	m.mu.Unlock()

	go m.streamOutput(tabID, stdout)
	go func(expected *connection) {
		_ = session.Wait()
		_ = m.disconnectConnection(tabID, expected)
	}(connState)

	return nil
}

func (m *Manager) SendInput(tabID, data string) error {
	m.mu.RLock()
	conn := m.connections[tabID]
	m.mu.RUnlock()
	if conn == nil {
		return fmt.Errorf("ssh tab %q is not connected", tabID)
	}
	if _, err := io.WriteString(conn.stdin, data); err != nil {
		return fmt.Errorf("write ssh input: %w", err)
	}
	return nil
}

func (m *Manager) ResizeTerminal(tabID string, cols, rows int) error {
	m.mu.RLock()
	conn := m.connections[tabID]
	m.mu.RUnlock()
	if conn == nil {
		return fmt.Errorf("ssh tab %q is not connected", tabID)
	}
	if err := conn.session.WindowChange(rows, cols); err != nil {
		return fmt.Errorf("resize ssh terminal: %w", err)
	}
	return nil
}

func (m *Manager) Disconnect(tabID string) error {
	m.mu.Lock()
	conn := m.connections[tabID]
	delete(m.connections, tabID)
	delete(m.handlers, tabID)
	delete(m.pendingKeys, tabID)
	m.mu.Unlock()
	return closeConnection(conn)
}

// disconnectConnection closes a connection only if it is still the active
// connection for the tab. This prevents an old session's Wait goroutine from
// tearing down a newer connection after reconnecting the same tab.
func (m *Manager) disconnectConnection(tabID string, expected *connection) error {
	m.mu.Lock()
	if current := m.connections[tabID]; current != expected {
		m.mu.Unlock()
		return nil
	}
	delete(m.connections, tabID)
	delete(m.handlers, tabID)
	delete(m.pendingKeys, tabID)
	m.mu.Unlock()
	return closeConnection(expected)
}

func closeConnection(conn *connection) error {
	if conn == nil {
		return nil
	}
	for _, listener := range conn.localListeners {
		_ = listener.Close()
	}
	if conn.stdin != nil {
		_ = conn.stdin.Close()
	}
	if conn.session != nil {
		_ = conn.session.Close()
	}
	if conn.client != nil {
		_ = conn.client.Close()
	}
	return nil
}

func (m *Manager) SetOutputHandler(tabID string, fn func(data string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.handlers[tabID] = fn
	if conn := m.connections[tabID]; conn != nil {
		conn.handler = fn
	}
}

func (m *Manager) GetCurrentDir(tabID string) (string, error) {
	m.mu.RLock()
	conn := m.connections[tabID]
	m.mu.RUnlock()
	if conn == nil {
		return "", fmt.Errorf("ssh tab %q is not connected", tabID)
	}
	session, err := conn.client.NewSession()
	if err != nil {
		return "", fmt.Errorf("create ssh pwd session: %w", err)
	}
	defer session.Close()
	output, err := session.CombinedOutput("pwd")
	if err != nil {
		return "", fmt.Errorf("run pwd over ssh: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

func (m *Manager) streamOutput(tabID string, reader io.Reader) {
	buffer := make([]byte, 4096)
	for {
		n, err := reader.Read(buffer)
		if n > 0 {
			m.mu.RLock()
			handler := m.handlers[tabID]
			m.mu.RUnlock()
			if handler != nil {
				handler(string(buffer[:n]))
			}
		}
		if err != nil {
			return
		}
	}
}

func (m *Manager) hostKeyCallback(tabID string) (xssh.HostKeyCallback, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve user home dir: %w", err)
	}
	knownHostsPath := filepath.Join(homeDir, ".ssh", "known_hosts")

	var trustedCb xssh.HostKeyCallback
	if _, err := os.Stat(knownHostsPath); !os.IsNotExist(err) {
		trustedCb, err = knownhosts.New(knownHostsPath)
		if err != nil {
			return nil, fmt.Errorf("load known_hosts: %w", err)
		}
	}

	return func(hostname string, remote net.Addr, key xssh.PublicKey) error {
		if trustedCb != nil {
			err := trustedCb(hostname, remote, key)
			if err == nil {
				return nil
			}
			var keyErr *knownhosts.KeyError
			if errors.As(err, &keyErr) && len(keyErr.Want) > 0 {
				// Key mismatch — return as-is (possible MITM).
				return err
			}
			// Host not found — fall through to prompt user.
		}
		fp := xssh.FingerprintSHA256(key)
		m.mu.Lock()
		m.pendingKeys[tabID] = &PendingHostKey{
			Hostname:    hostname,
			Remote:      remote,
			Key:         key,
			Fingerprint: fp,
			KeyType:     key.Type(),
		}
		m.mu.Unlock()
		return fmt.Errorf("%s: %s fingerprint %s", ErrUnknownHostKey, hostname, fp)
	}, nil
}

// GetPendingHostKey returns the pending host key for the given tab, if any.
func (m *Manager) GetPendingHostKey(tabID string) *PendingHostKey {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.pendingKeys[tabID]
}

// AcceptHostKey appends the pending host key for tabID to known_hosts and
// removes it from the pending map.
func (m *Manager) AcceptHostKey(tabID string) error {
	m.mu.RLock()
	pending := m.pendingKeys[tabID]
	m.mu.RUnlock()

	if pending == nil {
		return fmt.Errorf("no pending host key for tab %q", tabID)
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve user home dir: %w", err)
	}
	sshDir := filepath.Join(homeDir, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		return fmt.Errorf("create .ssh dir: %w", err)
	}
	knownHostsPath := filepath.Join(sshDir, "known_hosts")
	f, err := os.OpenFile(knownHostsPath, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		return fmt.Errorf("open known_hosts: %w", err)
	}
	defer f.Close()
	line := knownhosts.Line([]string{pending.Hostname}, pending.Key)
	if _, err := fmt.Fprintln(f, line); err != nil {
		return fmt.Errorf("write known_hosts: %w", err)
	}
	m.mu.Lock()
	if current := m.pendingKeys[tabID]; current == pending {
		delete(m.pendingKeys, tabID)
	}
	m.mu.Unlock()
	return nil
}

func buildClientConfig(user, password string, options map[string]string, hostKeyCallback xssh.HostKeyCallback) (*xssh.ClientConfig, error) {
	authMethods, err := sshauth.BuildAuthMethods(password, options)
	if err != nil {
		return nil, err
	}
	return &xssh.ClientConfig{
		User:            user,
		Auth:            authMethods,
		HostKeyCallback: hostKeyCallback,
	}, nil
}

func dialTarget(ctx context.Context, address string, config *xssh.ClientConfig, options map[string]string) (net.Conn, error) {
	proxyJump := strings.TrimSpace(optionValue(options, "proxy_jump"))
	if proxyJump == "" {
		dialer := &net.Dialer{}
		netConn, err := dialer.DialContext(ctx, "tcp", address)
		if err != nil {
			return nil, fmt.Errorf("dial ssh server: %w", err)
		}
		return netConn, nil
	}
	jumps := parseProxyJumps(proxyJump, config.User)
	if len(jumps) == 0 {
		return nil, fmt.Errorf("proxy_jump contains no valid hosts")
	}
	clients := make([]*xssh.Client, 0, len(jumps))
	closeClients := func() {
		for i := len(clients)-1; i >= 0; i-- { _ = clients[i].Close() }
	}
	firstCfg := cloneClientConfig(config)
	firstCfg.User = jumps[0].User
	client, err := xssh.Dial("tcp", jumps[0].Address, firstCfg)
	if err != nil { return nil, fmt.Errorf("dial proxy jump %s: %w", jumps[0].Address, err) }
	clients = append(clients, client)
	for _, jump := range jumps[1:] {
		conn, err := clients[len(clients)-1].Dial("tcp", jump.Address)
		if err != nil { closeClients(); return nil, fmt.Errorf("connect proxy jump %s: %w", jump.Address, err) }
		jumpCfg := cloneClientConfig(config)
		jumpCfg.User = jump.User
		sshConn, chans, reqs, err := xssh.NewClientConn(conn, jump.Address, jumpCfg)
		if err != nil { _ = conn.Close(); closeClients(); return nil, fmt.Errorf("authenticate proxy jump %s: %w", jump.Address, err) }
		clients = append(clients, xssh.NewClient(sshConn, chans, reqs))
	}
	netConn, err := clients[len(clients)-1].Dial("tcp", address)
	if err != nil { closeClients(); return nil, fmt.Errorf("dial target via proxy jump: %w", err) }
	return &proxyConn{Conn: netConn, jumpClients: clients}, nil
}

type proxyJump struct {
	Address string
	User    string
}

func parseProxyJumps(value, defaultUser string) []proxyJump {
	parts := strings.Split(value, ",")
	result := make([]proxyJump, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" { continue }
		user := defaultUser
		hostPort := part
		if at := strings.Index(part, "@"); at > 0 {
			user = strings.TrimSpace(part[:at])
			hostPort = strings.TrimSpace(part[at+1:])
		}
		host, port, err := net.SplitHostPort(hostPort)
		if err != nil || host == "" {
			host = hostPort
			port = "22"
		}
		if host == "" { continue }
		result = append(result, proxyJump{Address: net.JoinHostPort(host, port), User: user})
	}
	return result
}

func startLocalForwards(client *xssh.Client, options map[string]string) ([]net.Listener, error) {
	if options == nil {
		return nil, nil
	}
	raw := strings.TrimSpace(options["local_forwards"])
	if raw == "" {
		return nil, nil
	}
	specs := strings.Split(raw, ",")
	listeners := make([]net.Listener, 0, len(specs))
	for _, spec := range specs {
		bindAddr, remoteAddr, err := parseLocalForwardSpec(spec)
		if err != nil {
			for _, listener := range listeners {
				_ = listener.Close()
			}
			return nil, err
		}
		listener, err := net.Listen("tcp", bindAddr)
		if err != nil {
			for _, current := range listeners {
				_ = current.Close()
			}
			return nil, fmt.Errorf("open local tunnel %s: %w", bindAddr, err)
		}
		listeners = append(listeners, listener)
		go handleTunnelListener(listener, client, remoteAddr)
	}
	return listeners, nil
}

func handleTunnelListener(listener net.Listener, client *xssh.Client, remoteAddr string) {
	for {
		localConn, err := listener.Accept()
		if err != nil {
			return
		}
		go func() {
			defer localConn.Close()
			remoteConn, err := client.Dial("tcp", remoteAddr)
			if err != nil {
				return
			}
			defer remoteConn.Close()
			go io.Copy(remoteConn, localConn)
			_, _ = io.Copy(localConn, remoteConn)
		}()
	}
}

func parseLocalForwardSpec(spec string) (string, string, error) {
	spec = strings.TrimSpace(spec)
	parts := strings.Split(spec, ":")
	if len(parts) != 3 {
		return "", "", fmt.Errorf("invalid local forward %q, expected localPort:remoteHost:remotePort", spec)
	}
	localPort := strings.TrimSpace(parts[0])
	remoteHost := strings.TrimSpace(parts[1])
	remotePort := strings.TrimSpace(parts[2])
	if localPort == "" || remoteHost == "" || remotePort == "" {
		return "", "", fmt.Errorf("invalid local forward %q", spec)
	}
	return net.JoinHostPort("127.0.0.1", localPort), net.JoinHostPort(remoteHost, remotePort), nil
}

func cloneClientConfig(config *xssh.ClientConfig) *xssh.ClientConfig {
	clone := *config
	clone.Auth = append([]xssh.AuthMethod(nil), config.Auth...)
	return &clone
}

func optionValue(options map[string]string, key string) string {
	if options == nil { return "" }
	return options[key]
}

type proxyConn struct {
	net.Conn
	jumpClients []*xssh.Client
}

func (c *proxyConn) Close() error {
	connErr := c.Conn.Close()
	for i := len(c.jumpClients)-1; i >= 0; i-- { _ = c.jumpClients[i].Close() }
	return connErr
}

