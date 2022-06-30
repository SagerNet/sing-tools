package main

import (
	"context"
	"encoding/json"
	"io/ioutil"
	"net"
	"net/netip"
	"os"
	"os/signal"
	"syscall"

	"github.com/sagernet/sing-shadowsocks"
	"github.com/sagernet/sing-shadowsocks/shadowaead"
	"github.com/sagernet/sing-shadowsocks/shadowaead_2022"
	_ "github.com/sagernet/sing-tools/extensions/log"
	"github.com/sagernet/sing-tools/extensions/transport/tcp"
	"github.com/sagernet/sing-tools/extensions/transport/udp"
	"github.com/sagernet/sing/common"
	"github.com/sagernet/sing/common/bufio"
	E "github.com/sagernet/sing/common/exceptions"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	"github.com/sagernet/sing/common/uot"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

type Flags struct {
	Server     string `json:"server"`
	ServerPort uint16 `json:"server_port"`
	Bind       string `json:"local_address"`
	LocalPort  uint16 `json:"local_port"`
	Password   string `json:"password"`
	// deprecated
	Key      string `json:"key"`
	Method   string `json:"method"`
	LogLevel string `json:"log_level"`
}

var configPath string

func main() {
	command := &cobra.Command{
		Use:   "ss-server [-c config.json]",
		Short: "shadowsocks server",
		Run:   run,
	}
	command.Flags().StringVarP(&configPath, "config", "c", "", "set a configuration file")
	err := command.Execute()
	if err != nil {
		logrus.Fatal(err)
	}
}

func run(cmd *cobra.Command, args []string) {
	if configPath == "" {
		configPath = "config.json"
	}

	configFile, err := ioutil.ReadFile(configPath)
	if err != nil {
		logrus.Fatal(E.Cause(err, "read config file"))
	}

	f := new(Flags)
	err = json.Unmarshal(configFile, f)
	if err != nil {
		logrus.Fatal(E.Cause(err, "parse config file"))
	}

	if f.LogLevel != "" {
		level, err := logrus.ParseLevel(f.LogLevel)
		if err != nil {
			logrus.Fatal("unknown log level ", f.LogLevel)
		}
		logrus.SetLevel(level)
	}

	s, err := newServer(f)
	if err != nil {
		logrus.Fatal(err)
	}

	err = s.Start()
	if err != nil {
		logrus.Fatal(err)
	}
	logrus.Info("server started at ", s.tcpIn.TCPListener.Addr())

	osSignals := make(chan os.Signal, 1)
	signal.Notify(osSignals, os.Interrupt, syscall.SIGTERM)
	<-osSignals

	s.Close()
}

type server struct {
	tcpIn   *tcp.Listener
	udpIn   *udp.Listener
	service shadowsocks.Service
}

func (s *server) Start() error {
	err := s.tcpIn.Start()
	if err != nil {
		return err
	}
	err = s.udpIn.Start()
	return err
}

func (s *server) Close() error {
	s.tcpIn.Close()
	s.udpIn.Close()
	return nil
}

func newServer(f *Flags) (*server, error) {
	s := new(server)

	if f.Server == "" {
		return nil, E.New("missing server address")
	} else if f.ServerPort == 0 {
		return nil, E.New("missing server port")
	} else if f.Method == "" {
		return nil, E.New("missing method")
	}

	if f.Key != "" {
		f.Password = f.Key
	}

	if f.Method == shadowsocks.MethodNone {
		s.service = shadowsocks.NewNoneService(300, s)
	} else if common.Contains(shadowaead.List, f.Method) {
		service, err := shadowaead.NewService(f.Method, nil, f.Password, 300, s)
		if err != nil {
			return nil, err
		}
		s.service = service
	} else if common.Contains(shadowaead_2022.List, f.Method) {
		service, err := shadowaead_2022.NewServiceWithPassword(f.Method, f.Password, 300, s)
		if err != nil {
			return nil, err
		}
		s.service = service
	} else {
		return nil, E.New("unsupported method " + f.Method)
	}

	var bind netip.Addr
	if f.Server != "" {
		addr, err := netip.ParseAddr(f.Server)
		if err != nil {
			return nil, E.Cause(err, "bad server address")
		}
		bind = addr
	} else {
		bind = netip.IPv6Unspecified()
	}
	s.tcpIn = tcp.NewTCPListener(netip.AddrPortFrom(bind, f.ServerPort), s.service)
	s.udpIn = udp.NewUDPListener(netip.AddrPortFrom(bind, f.ServerPort), s.service)
	return s, nil
}

func (s *server) NewConnection(ctx context.Context, conn net.Conn, metadata M.Metadata) error {
	if metadata.Destination.Fqdn == uot.UOTMagicAddress {
		logrus.Info("inbound UOT from ", conn.RemoteAddr())

		udpConn, err := net.ListenUDP("udp", nil)
		if err != nil {
			return err
		}
		return bufio.CopyConn(ctx, conn, uot.NewServerConn(udpConn))
	}

	logrus.Info("inbound TCP ", conn.RemoteAddr(), " ==> ", metadata.Destination)
	destConn, err := N.SystemDialer.DialContext(ctx, "tcp", metadata.Destination)
	if err != nil {
		return err
	}
	return bufio.CopyConn(ctx, conn, destConn)
}

func (s *server) NewPacketConnection(ctx context.Context, conn N.PacketConn, metadata M.Metadata) error {
	logrus.Info("inbound UDP ", metadata.Source, " ==> ", metadata.Destination)
	udpConn, err := net.ListenUDP("udp", nil)
	if err != nil {
		return err
	}
	return bufio.CopyPacketConn(ctx, conn, bufio.NewPacketConn(udpConn))
}

func (s *server) HandleError(err error) {
	common.Close(err)
	if E.IsClosed(err) {
		return
	}
	logrus.Warn(err)
}
