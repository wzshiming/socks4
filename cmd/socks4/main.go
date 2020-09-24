package main

import (
	"flag"
	"log"
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
	logger := log.New(os.Stderr, "[socks4] ", log.LstdFlags)
	svc := &socks4.Server{
		Logger: logger,
	}
	if username != "" {
		svc.Authentication = socks4.UserAuth(username)
	}
	err := svc.ListenAndServe("tcp", address)
	if err != nil {
		logger.Println(err)
	}
}
