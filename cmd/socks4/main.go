package main

import (
	"flag"
	"log/slog"
	"os"

	"github.com/wzshiming/socks4"
)

var address string
var username string

func init() {
	flag.StringVar(&address, "a", ":1080", "listen on the address")
	flag.StringVar(&username, "u", "", "username")
	flag.Parse()
}

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	svc := &socks4.Server{
		Logger: logger,
	}
	if username != "" {
		svc.Authentication = socks4.UserAuth(username)
	}
	err := svc.ListenAndServe("tcp", address)
	if err != nil {
		logger.Error("listen and serve", "err", err)
		os.Exit(1)
	}
}
