package main

import (
	"context"
	"crypto/tls"
	"flag"
	"log"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/pion/dtls/v3"
	"github.com/pion/dtls/v3/pkg/crypto/selfsign"
)

func main() {
	listen := flag.String("listen", "0.0.0.0:56000", "listen on ip:port")
	connect := flag.String("connect", "", "connect to ip:port")
	udpOnly := flag.Bool("udp", true, "udp-only mode")
	flag.Parse()
	_ = udpOnly
	if *connect == "" {
		log.Fatal("-connect is required")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM)
	go func() { <-sc; cancel() }()

	addr, err := net.ResolveUDPAddr("udp", *listen)
	if err != nil {
		log.Fatal(err)
	}
	cert, err := selfsign.GenerateSelfSigned()
	if err != nil {
		log.Fatal(err)
	}
	ln, err := dtls.Listen("udp", addr, &dtls.Config{Certificates: []tls.Certificate{cert}, ExtendedMasterSecret: dtls.RequireExtendedMasterSecret, CipherSuites: []dtls.CipherSuiteID{dtls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256}})
	if err != nil {
		log.Fatal(err)
	}
	defer ln.Close()

	var wg sync.WaitGroup
	for {
		c, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			continue
		}
		wg.Add(1)
		go func(conn net.Conn) {
			defer wg.Done()
			defer conn.Close()
			dc := conn.(*dtls.Conn)
			hctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			if err := dc.HandshakeContext(hctx); err != nil {
				return
			}
			out, err := net.Dial("udp", *connect)
			if err != nil {
				return
			}
			defer out.Close()
			proxy(ctx, dc, out)
		}(c)
	}
	wg.Wait()
}

func proxy(ctx context.Context, a net.Conn, b net.Conn) {
	var wg sync.WaitGroup
	wg.Add(2)
	cp := func(dst net.Conn, src net.Conn) {
		defer wg.Done()
		buf := make([]byte, 1600)
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			src.SetReadDeadline(time.Now().Add(30 * time.Second))
			n, err := src.Read(buf)
			if err != nil {
				return
			}
			dst.SetWriteDeadline(time.Now().Add(30 * time.Second))
			if _, err := dst.Write(buf[:n]); err != nil {
				return
			}
		}
	}
	go cp(a, b)
	go cp(b, a)
	wg.Wait()
}
