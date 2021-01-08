package socks4

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"time"
)

// Dialer is a SOCKS4 dialer.
type Dialer struct {
	// ProxyNetwork network between a proxy server and a client
	ProxyNetwork string
	// ProxyAddress proxy server address
	ProxyAddress string
	// ProxyDial specifies the optional dial function for
	// establishing the transport connection.
	ProxyDial func(context.Context, string, string) (net.Conn, error)
	// Username use username authentication if not empty
	Username string
	// IsResolve resolve domain name on locally
	IsResolve bool
	// Resolver optionally specifies an alternate resolver to use
	Resolver *net.Resolver
	// Timeout is the maximum amount of time a dial will wait for
	// a connect to complete. The default is no timeout
	Timeout time.Duration
}

// NewDialer returns a new Dialer that dials through the provided
// proxy server's network and address.
func NewDialer(addr string) (*Dialer, error) {
	d := &Dialer{
		ProxyNetwork: "tcp",
		Timeout:      time.Minute,
	}
	u, err := url.Parse(addr)
	if err != nil {
		return nil, err
	}
	switch u.Scheme {
	case "socks4":
		d.IsResolve = true
	case "socks4a":
	default:
		return nil, fmt.Errorf("unsupported protocol '%s'", u.Scheme)
	}
	host := u.Host
	port := u.Port()
	if port == "" {
		port = "1080"
		hostname := u.Hostname()
		host = net.JoinHostPort(hostname, port)
	}
	if u.User != nil {
		d.Username = u.User.Username()
	}
	d.ProxyAddress = host
	return d, nil
}

// DialContext connects to the provided address on the provided network.
func (d *Dialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	switch network {
	default:
		return nil, fmt.Errorf("unsupported network %q", network)
	case "tcp", "tcp4", "tcp6":
		return d.do(ctx, ConnectCommand, address)
	}
}

// Dial connects to the provided address on the provided network.
func (d *Dialer) Dial(network, address string) (net.Conn, error) {
	return d.DialContext(context.Background(), network, address)
}

func (d *Dialer) Listen(ctx context.Context, network, address string) (net.Listener, error) {
	switch network {
	default:
		return nil, fmt.Errorf("unsupported network %q", network)
	case "tcp", "tcp4", "tcp6":
	}
	return &listener{ctx: ctx, d: d, address: address}, nil
}

// DialContext connects to the provided address on the provided network.
func (d *Dialer) do(ctx context.Context, cmd Command, address string) (net.Conn, error) {
	if d.IsResolve {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		if host != "" {
			ip := net.ParseIP(host)
			if ip == nil {
				ipaddr, err := d.resolver().LookupIP(ctx, "ip4", host)
				if err != nil {
					return nil, err
				}
				host := ipaddr[0].String()
				address = net.JoinHostPort(host, port)
			}
		}
	}

	conn, err := d.proxyDial(ctx, d.ProxyNetwork, d.ProxyAddress)
	if err != nil {
		return nil, err
	}

	_, err = d.connect(ctx, conn, cmd, address)
	if err != nil {
		conn.Close()
		return nil, err
	}

	return conn, nil
}

func (d *Dialer) connect(ctx context.Context, conn net.Conn, cmd Command, address string) (net.Addr, error) {
	if d.Timeout != 0 {
		deadline := time.Now().Add(d.Timeout)
		if d, ok := ctx.Deadline(); !ok || deadline.Before(d) {
			subCtx, cancel := context.WithDeadline(ctx, deadline)
			defer cancel()
			ctx = subCtx
		}
	}
	if deadline, ok := ctx.Deadline(); ok && !deadline.IsZero() {
		conn.SetDeadline(deadline)
		defer conn.SetDeadline(time.Time{})
	}

	_, err := conn.Write([]byte{socks4Version, byte(cmd)})
	if err != nil {
		return nil, err
	}

	err = writeAddrAndUserWithStr(conn, address, d.Username)
	if err != nil {
		return nil, err
	}
	return d.readReply(conn)
}

func (d *Dialer) readReply(conn net.Conn) (net.Addr, error) {
	var header [2]byte
	i, err := io.ReadFull(conn, header[:])
	if err != nil {
		return nil, err
	}
	if i != 2 {
		return nil, errors.New("server does not respond properly")
	}
	addr, err := readAddr(conn)
	if err != nil {
		return nil, err
	}

	rep := reply(header[1])
	if rep != grantedReply {
		return nil, fmt.Errorf("socks connection request failed: %s", rep)
	}
	return addr, nil
}

func (d *Dialer) resolver() *net.Resolver {
	if d.Resolver == nil {
		return net.DefaultResolver
	}
	return d.Resolver
}

func (d *Dialer) proxyDial(ctx context.Context, network, address string) (net.Conn, error) {
	proxyDial := d.ProxyDial
	if proxyDial == nil {
		var dialer net.Dialer
		proxyDial = dialer.DialContext
	}
	return proxyDial(ctx, network, address)
}

type listener struct {
	ctx     context.Context
	d       *Dialer
	address string
}

// Accept waits for and returns the next connection to the listener.
func (l *listener) Accept() (net.Conn, error) {
	conn, err := l.d.do(l.ctx, BindCommand, l.address)
	if err != nil {
		return nil, err
	}
	addr, err := l.d.readReply(conn)
	if err != nil {
		return nil, err
	}
	return &connect{Conn: conn, remoteAddr: addr}, nil
}

// Close closes the listener.
func (l *listener) Close() error {
	return nil
}

// address returns the listener's network address.
func (l *listener) Addr() net.Addr {
	return nil
}

type connect struct {
	net.Conn
	remoteAddr net.Addr
}

func (c *connect) RemoteAddr() net.Addr {
	return c.remoteAddr
}
