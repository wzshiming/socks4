package socks4

import (
	"context"
	"fmt"
	"net"
	"net/url"
)

// SimpleServer is a simplified server, which can be configured as easily as client.
type SimpleServer struct {
	Server
	Listener net.Listener
	Network  string
	Address  string
	Username string
}

// NewServer creates a new NewSimpleServer
func NewSimpleServer(addr string) (*SimpleServer, error) {
	s := &SimpleServer{}
	u, err := url.Parse(addr)
	if err != nil {
		return nil, err
	}
	switch u.Scheme {
	case "socks4", "socks4a":
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
		s.Username = u.User.Username()
		s.Authentication = UserAuth(s.Username)
	}

	s.Address = host
	s.Network = "tcp"
	return s, nil
}

// Run the server
func (s *SimpleServer) Run(ctx context.Context) error {
	var listenConfig net.ListenConfig
	listener, err := listenConfig.Listen(ctx, s.Network, s.Address)
	if err != nil {
		return err
	}
	s.Listener = listener
	s.Address = listener.Addr().String()
	return s.Serve(listener)
}

// Start the server
func (s *SimpleServer) Start(ctx context.Context) error {
	var listenConfig net.ListenConfig
	listener, err := listenConfig.Listen(ctx, s.Network, s.Address)
	if err != nil {
		return err
	}
	s.Listener = listener
	s.Address = listener.Addr().String()
	go s.Serve(listener)
	return nil
}

// Close closes the listener
func (s *SimpleServer) Close() error {
	if s.Listener == nil {
		return nil
	}
	return s.Listener.Close()
}

// ProxyURL returns the URL of the proxy
func (s *SimpleServer) ProxyURL() string {
	u := url.URL{
		Scheme: "socks4",
		Host:   s.Address,
	}
	if s.Username != "" {
		u.User = url.User(s.Username)
	}
	return u.String()
}
