package main

import (
	"context"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/go-acme/lego/v4/log"
	"github.com/sagernet/sing/common"
	"github.com/sagernet/sing/common/bufio"
	E "github.com/sagernet/sing/common/exceptions"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	"github.com/sagernet/sing/common/redir"
	"github.com/sagernet/sing/transport/mixed"
)

func main() {
	server := mixed.NewListener(M.ParseSocksaddr(os.Args[1]).AddrPort(), nil, redir.ModeDisabled, 500, &proxyHandler{})
	err := server.Start()
	if err != nil {
		log.Fatal(err)
	}
	osSignals := make(chan os.Signal, 1)
	signal.Notify(osSignals, os.Interrupt, syscall.SIGTERM)
	<-osSignals
	server.Close()
}

type proxyHandler struct{}

func (h *proxyHandler) NewConnection(ctx context.Context, conn net.Conn, metadata M.Metadata) error {
	destConn, err := N.SystemDialer.DialContext(ctx, "tcp", metadata.Destination)
	if err != nil {
		return err
	}
	return bufio.CopyConn(ctx, conn, destConn)
}

func (h *proxyHandler) NewPacketConnection(ctx context.Context, conn N.PacketConn, metadata M.Metadata) error {
	udpConn, err := net.ListenUDP("udp", nil)
	if err != nil {
		return err
	}
	return bufio.CopyPacketConn(ctx, conn, bufio.NewPacketConn(udpConn))
}

func (h *proxyHandler) HandleError(err error) {
	common.Close(err)
	if E.IsClosed(err) {
		return
	}
	log.Println(err)
}
