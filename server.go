package socks4

import (
	"context"
	"fmt"
	"io"
	"net"
)

// Server is accepting connections and handling the details of the SOCKS4 protocol
type Server struct {
	// Authentication is proxy authentication
	Authentication Authentication
	// ProxyDial specifies the optional proxyDial function for
	// establishing the transport connection.
	ProxyDial func(ctx context.Context, network string, address string) (net.Conn, error)
	// Logger error log
	Logger Logger
	// Context is default context
	Context context.Context
	// BytesPool getting and returning temporary bytes for use by io.CopyBuffer
	BytesPool BytesPool
}

type Logger interface {
	Println(v ...interface{})
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
	req.Command = Command(cmd)

	addr, err := readAddrAndUser(conn)
	if err != nil {
		if err := sendReply(req.Conn, rejectedReply, nil); err != nil {
			return fmt.Errorf("failed to send reply: %v", err)
		}
		return err
	}
	req.DestinationAddr = &addr.address
	req.Username = addr.Username
	if s.Authentication != nil && !s.Authentication.Auth(req.Command, req.Username) {
		if err := sendReply(req.Conn, invalidUserReply, nil); err != nil {
			return fmt.Errorf("failed to send reply: %v", err)
		}
		return errUserAuthFailed
	}
	return s.handle(req)
}

func (s *Server) handle(req *request) error {
	switch req.Command {
	case ConnectCommand:
		return s.handleConnect(req)
	case BindCommand:
		return s.handleBind(req)
	default:
		if err := sendReply(req.Conn, rejectedReply, nil); err != nil {
			return err
		}
		return fmt.Errorf("unsupported Command: %v", req.Command)
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
	bind := address{IP: local.IP, Port: local.Port}
	if err := sendReply(req.Conn, grantedReply, &bind); err != nil {
		return fmt.Errorf("failed to send reply: %v", err)
	}

	var buf1, buf2 []byte
	if s.BytesPool != nil {
		buf1 = s.BytesPool.Get()
		buf2 = s.BytesPool.Get()
		defer func() {
			s.BytesPool.Put(buf1)
			s.BytesPool.Put(buf2)
		}()
	} else {
		buf1 = make([]byte, 32*1024)
		buf2 = make([]byte, 32*1024)
	}
	return tunnel(ctx, target, req.Conn, buf1, buf2)
}

func (s *Server) handleBind(req *request) error {
	ctx := s.context()
	var lc net.ListenConfig
	listener, err := lc.Listen(ctx, "tcp", req.DestinationAddr.String())
	if err != nil {
		if err := sendReply(req.Conn, rejectedReply, nil); err != nil {
			return fmt.Errorf("failed to send reply: %v", err)
		}
		return fmt.Errorf("connect to %v failed: %w", req.DestinationAddr, err)
	}

	localAddr := listener.Addr()
	local, ok := localAddr.(*net.TCPAddr)
	if !ok {
		listener.Close()
		return fmt.Errorf("connect to %v failed: local address is %s://%s", req.DestinationAddr, localAddr.Network(), localAddr.String())
	}
	bind := address{IP: local.IP, Port: local.Port}
	if err := sendReply(req.Conn, grantedReply, &bind); err != nil {
		listener.Close()
		return fmt.Errorf("failed to send reply: %v", err)
	}

	conn, err := listener.Accept()
	if err != nil {
		listener.Close()
		if err := sendReply(req.Conn, rejectedReply, nil); err != nil {
			return fmt.Errorf("failed to send reply: %v", err)
		}
		return fmt.Errorf("connect to %v failed: %w", req.DestinationAddr, err)
	}
	listener.Close()

	remoteAddr := conn.RemoteAddr()
	local, ok = remoteAddr.(*net.TCPAddr)
	if !ok {
		return fmt.Errorf("connect to %v failed: remote address is %s://%s", req.DestinationAddr, localAddr.Network(), localAddr.String())
	}
	bind = address{IP: local.IP, Port: local.Port}
	if err := sendReply(req.Conn, grantedReply, &bind); err != nil {
		return fmt.Errorf("failed to send reply: %v", err)
	}

	var buf1, buf2 []byte
	if s.BytesPool != nil {
		buf1 = s.BytesPool.Get()
		buf2 = s.BytesPool.Get()
		defer func() {
			s.BytesPool.Put(buf1)
			s.BytesPool.Put(buf2)
		}()
	} else {
		buf1 = make([]byte, 32*1024)
		buf2 = make([]byte, 32*1024)
	}
	return tunnel(ctx, conn, req.Conn, buf1, buf2)
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

func sendReply(w io.Writer, resp reply, addr *address) error {
	_, err := w.Write([]byte{0, byte(resp)})
	if err != nil {
		return err
	}
	err = writeAddr(w, addr)
	return err
}

type request struct {
	Version         uint8
	Command         Command
	DestinationAddr *address
	Username        string
	Conn            net.Conn
}
