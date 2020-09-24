package socks4

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
)

// Server is accepting connections and handling the details of the SOCKS4 protocol
type Server struct {
	// Authentication is proxy authentication
	Authentication Authentication
	// ProxyDial specifies the optional proxyDial function for
	// establishing the transport connection.
	ProxyDial func(context.Context, string, string) (net.Conn, error)
	// Logger error log
	Logger *log.Logger
	// Context is default context
	Context context.Context
}

// NewServer creates a new Server
func NewServer() *Server {
	return &Server{}
}

// ListenAndServe is used to create a listener and serve on it
func (s *Server) ListenAndServe(network, addr string) error {
	var lc net.ListenConfig
	l, err := lc.Listen(s.context(), network, addr)
	if err != nil {
		return err
	}
	return s.Serve(l)
}

// Serve is used to serve connections from a listener
func (s *Server) Serve(l net.Listener) error {
	for {
		conn, err := l.Accept()
		if err != nil {
			return err
		}
		go s.ServeConn(conn)
	}
}

// ServeConn is used to serve a single connection.
func (s *Server) ServeConn(conn net.Conn) {
	defer conn.Close()
	err := s.serveConn(conn)
	if err != nil && s.Logger != nil && !isClosedConnError(err) {
		s.Logger.Println(err)
	}
}

func (s *Server) serveConn(conn net.Conn) error {
	version, err := readByte(conn)
	if err != nil {
		return err
	}
	if version != socks4Version {
		return fmt.Errorf("unsupported SOCKS version: %d", version)
	}
	req := &request{
		Version: socks4Version,
		Conn:    conn,
	}

	cmd, err := readByte(conn)
	if err != nil {
		return err
	}
	req.Command = command(cmd)

	addr, err := readAddrAndUser(conn)
	if err != nil {
		if err := sendReply(req.Conn, rejectedReply, nil); err != nil {
			return fmt.Errorf("failed to send reply: %v", err)
		}
		return err
	}
	req.DestinationAddr = &addr.Addr
	req.Username = addr.Username
	if s.Authentication != nil && !s.Authentication.Auth(req.Username) {
		if err := sendReply(req.Conn, invalidUserReply, nil); err != nil {
			return fmt.Errorf("failed to send reply: %v", err)
		}
		return errUserAuthFailed
	}
	return s.handle(req)
}

func (s *Server) handle(req *request) error {
	switch req.Command {
	case connectCommand:
		return s.handleConnect(req)
	//case bindCommand:
	default:
		if err := sendReply(req.Conn, rejectedReply, nil); err != nil {
			return err
		}
		return fmt.Errorf("unsupported command: %v", req.Command)
	}
}

func (s *Server) handleConnect(req *request) error {
	ctx := s.context()
	target, err := s.proxyDial(ctx, "tcp", req.DestinationAddr.Address())
	if err != nil {
		if err := sendReply(req.Conn, rejectedReply, nil); err != nil {
			return fmt.Errorf("failed to send reply: %v", err)
		}
		return fmt.Errorf("connect to %v failed: %w", req.DestinationAddr, err)
	}

	local := target.LocalAddr().(*net.TCPAddr)
	bind := Addr{IP: local.IP, Port: local.Port}
	if err := sendReply(req.Conn, grantedReply, &bind); err != nil {
		return fmt.Errorf("failed to send reply: %v", err)
	}
	return tunnel(ctx, target, req.Conn)
}

func (s *Server) proxyDial(ctx context.Context, network, address string) (net.Conn, error) {
	proxyDial := s.ProxyDial
	if proxyDial == nil {
		var dialer net.Dialer
		proxyDial = dialer.DialContext
	}
	return proxyDial(ctx, network, address)
}

func (s *Server) context() context.Context {
	if s.Context == nil {
		return context.Background()
	}
	return s.Context
}

func sendReply(w io.Writer, resp reply, addr *Addr) error {
	_, err := w.Write([]byte{0, byte(resp)})
	if err != nil {
		return err
	}
	err = writeAddr(w, addr)
	return err
}

type request struct {
	Version         uint8
	Command         command
	DestinationAddr *Addr
	Username        string
	Conn            net.Conn
}
