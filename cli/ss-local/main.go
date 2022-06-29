package main

import (
	"context"
	"encoding/json"
	"io/ioutil"
	"net"
	"net/netip"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/sagernet/sing-shadowsocks"
	"github.com/sagernet/sing-shadowsocks/shadowaead"
	"github.com/sagernet/sing-shadowsocks/shadowaead_2022"
	"github.com/sagernet/sing-shadowsocks/shadowimpl"
	"github.com/sagernet/sing-shadowsocks/shadowstream"
	"github.com/sagernet/sing/common"
	"github.com/sagernet/sing/common/buf"
	"github.com/sagernet/sing/common/bufio"
	E "github.com/sagernet/sing/common/exceptions"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	"github.com/sagernet/sing/common/redir"
	"github.com/sagernet/sing/common/udpnat"
	"github.com/sagernet/sing/transport/mixed"
	"github.com/sagernet/sing/transport/system"
	"github.com/sagernet/sing/transport/tcp"
	"github.com/sagernet/sing/transport/udp"
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
	Key         string `json:"key"`
	Method      string `json:"method"`
	TCPFastOpen bool   `json:"fast_open"`
	Verbose     bool   `json:"verbose"`
	Transproxy  string `json:"transproxy"`
	FWMark      int    `json:"fwmark"`
	Tunnel      string `json:"tunnel"`
	ConfigFile  string
}

func main() {
	f := new(Flags)

	command := &cobra.Command{
		Use:   "ss-local",
		Short: "shadowsocks client",
		Run: func(cmd *cobra.Command, args []string) {
			run(cmd, f)
		},
	}

	command.Flags().StringVarP(&f.Server, "server", "s", "", "Store the server’s hostname or IP.")
	command.Flags().Uint16VarP(&f.ServerPort, "server-port", "p", 0, "Store the server’s port number.")
	command.Flags().StringVarP(&f.Bind, "local-address", "b", "", "Store the local address.")
	command.Flags().Uint16VarP(&f.LocalPort, "local-port", "l", 0, "Store the local port number.")
	command.Flags().StringVarP(&f.Password, "password", "k", "", "Store the password. The server and the client should use the same password.")
	command.Flags().StringVar(&f.Key, "key", "", "Store the key directly. The key should be encoded with URL-safe Base64.")

	var supportedCiphers []string
	supportedCiphers = append(supportedCiphers, shadowsocks.MethodNone)
	supportedCiphers = append(supportedCiphers, shadowaead_2022.List...)
	supportedCiphers = append(supportedCiphers, shadowaead.List...)
	supportedCiphers = append(supportedCiphers, shadowstream.List...)

	command.Flags().StringVarP(&f.Method, "encrypt-method", "m", "", "Store the cipher.\n\nSupported ciphers:\n\n"+strings.Join(supportedCiphers, "\n"))
	command.Flags().BoolVar(&f.TCPFastOpen, "fast-open", false, `Enable TCP fast open.`)
	command.Flags().StringVar(&f.Tunnel, "tunnel", "", "Enable tunnel mode.")
	command.Flags().StringVarP(&f.Transproxy, "transproxy", "t", "", "Enable transparent proxy support. [possible values: redirect, tproxy]")
	command.Flags().IntVar(&f.FWMark, "fwmark", 0, "Store outbound socket mark.")
	command.Flags().StringVarP(&f.ConfigFile, "config", "c", "", "Use a configuration file.")
	command.Flags().BoolVarP(&f.Verbose, "verbose", "v", false, "Enable verbose mode.")
	err := command.Execute()
	if err != nil {
		logrus.Fatal(err)
	}
}

type Client struct {
	mixIn     *mixed.Listener
	tcpIn     *tcp.Listener
	udpIn     *udp.Listener
	server    M.Socksaddr
	method    shadowsocks.Method
	dialer    net.Dialer
	isTunnel  bool
	tunnel    M.Socksaddr
	tunnelNat *udpnat.Service[netip.AddrPort]
}

func (c *Client) Start() error {
	if !c.isTunnel {
		return c.mixIn.Start()
	} else {
		err := c.tcpIn.Start()
		if err != nil {
			return err
		}
		return c.udpIn.Start()
	}
}

func (c *Client) Close() error {
	if !c.isTunnel {
		return c.mixIn.Close()
	} else {
		return common.Close(c.tcpIn, c.udpIn)
	}
}

func newClient(f *Flags) (*Client, error) {
	if f.ConfigFile != "" {
		configFile, err := ioutil.ReadFile(f.ConfigFile)
		if err != nil {
			return nil, E.Cause(err, "read config file")
		}
		flagsNew := new(Flags)
		err = json.Unmarshal(configFile, flagsNew)
		if err != nil {
			return nil, E.Cause(err, "decode config file")
		}
		if flagsNew.Server != "" && f.Server == "" {
			f.Server = flagsNew.Server
		}
		if flagsNew.ServerPort != 0 && f.ServerPort == 0 {
			f.ServerPort = flagsNew.ServerPort
		}
		if flagsNew.Bind != "" && f.Bind == "" {
			f.Bind = flagsNew.Bind
		}
		if flagsNew.LocalPort != 0 && f.LocalPort == 0 {
			f.LocalPort = flagsNew.LocalPort
		}
		if flagsNew.Password != "" && f.Password == "" {
			f.Password = flagsNew.Password
		}
		if flagsNew.Key != "" && f.Key == "" {
			f.Key = flagsNew.Key
		}
		if flagsNew.Method != "" && f.Method == "" {
			f.Method = flagsNew.Method
		}
		if flagsNew.Transproxy != "" && f.Transproxy == "" {
			f.Transproxy = flagsNew.Transproxy
		}
		if flagsNew.Tunnel != "" && f.Tunnel == "" {
			f.Tunnel = flagsNew.Tunnel
		}
		if flagsNew.TCPFastOpen {
			f.TCPFastOpen = true
		}
		if flagsNew.Verbose {
			f.Verbose = true
		}
	}

	if f.Verbose {
		logrus.SetLevel(logrus.TraceLevel)
	}

	if f.Server == "" {
		return nil, E.New("missing server address")
	} else if f.ServerPort == 0 {
		return nil, E.New("missing server port")
	} else if f.Method == "" {
		return nil, E.New("missing method")
	}

	c := &Client{
		server: M.ParseSocksaddrHostPort(f.Server, f.ServerPort),
		dialer: net.Dialer{
			Timeout: 5 * time.Second,
		},
	}

	if f.Method == shadowsocks.MethodNone {
		c.method = shadowsocks.NewNone()
	} else {
		if f.Key != "" {
			f.Password = f.Key
		}
		method, err := shadowimpl.FetchMethod(f.Method, f.Password)
		if err != nil {
			return nil, err
		}
		c.method = method
	}

	c.dialer.Control = func(network, address string, c syscall.RawConn) error {
		var rawFd uintptr
		err := c.Control(func(fd uintptr) {
			rawFd = fd
		})
		if err != nil {
			return err
		}
		if f.FWMark > 0 {
			err = redir.FWMark(rawFd, f.FWMark)
			if err != nil {
				return err
			}
		}
		if f.TCPFastOpen {
			err = system.TCPFastOpen(rawFd)
			if err != nil {
				return err
			}
		}
		return nil
	}

	var bindAddr netip.Addr
	if f.Bind != "" {
		addr, err := netip.ParseAddr(f.Bind)
		if err != nil {
			return nil, E.Cause(err, "bad local address")
		}
		bindAddr = addr
	} else {
		bindAddr = netip.IPv6Unspecified()
	}
	bind := netip.AddrPortFrom(bindAddr, f.LocalPort)

	if f.Tunnel == "" {
		var transproxyMode redir.TransproxyMode
		switch f.Transproxy {
		case "redirect":
			transproxyMode = redir.ModeRedirect
		case "tproxy":
			transproxyMode = redir.ModeTProxy
		case "":
			transproxyMode = redir.ModeDisabled
		default:
			return nil, E.New("unknown transproxy mode ", f.Transproxy)
		}

		c.mixIn = mixed.NewListener(bind, nil, transproxyMode, 300, c)
	} else {
		c.isTunnel = true
		c.tunnel = M.ParseSocksaddr(f.Tunnel)
		c.tcpIn = tcp.NewTCPListener(bind, c)
		c.udpIn = udp.NewUDPListener(bind, c)
		c.tunnelNat = udpnat.New[netip.AddrPort](500, c)
	}

	return c, nil
}

func (c *Client) NewConnection(ctx context.Context, conn net.Conn, metadata M.Metadata) error {
	if c.isTunnel {
		metadata.Protocol = "tunnel"
		metadata.Destination = c.tunnel
	}

	logrus.Info("outbound ", metadata.Protocol, " TCP ", conn.RemoteAddr(), " ==> ", metadata.Destination)

	serverConn, err := c.dialer.DialContext(ctx, "tcp", c.server.String())
	if err != nil {
		return E.Cause(err, "connect to server")
	}
	_payload := buf.StackNew()
	payload := common.Dup(_payload)
	err = conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	if err != nil {
		return err
	}
	_, err = payload.ReadFrom(conn)
	if err != nil && !E.IsTimeout(err) {
		return E.Cause(err, "read payload")
	}
	err = conn.SetReadDeadline(time.Time{})
	if err != nil {
		payload.Release()
		return err
	}
	serverConn = c.method.DialEarlyConn(serverConn, metadata.Destination)
	_, err = serverConn.Write(payload.Bytes())
	if err != nil {
		return E.Cause(err, "client handshake")
	}
	runtime.KeepAlive(_payload)
	return bufio.CopyConn(ctx, serverConn, conn)
}

func (c *Client) NewPacketConnection(ctx context.Context, conn N.PacketConn, metadata M.Metadata) error {
	logrus.Info("outbound ", metadata.Protocol, " UDP ", metadata.Source, " ==> ", metadata.Destination)
	udpConn, err := c.dialer.DialContext(ctx, "udp", c.server.String())
	if err != nil {
		return err
	}
	serverConn := c.method.DialPacketConn(udpConn)
	return bufio.CopyPacketConn(ctx, serverConn, conn)
}

func (c *Client) WriteIsThreadUnsafe() {
}

func (c *Client) NewPacket(ctx context.Context, conn N.PacketConn, buffer *buf.Buffer, metadata M.Metadata) error {
	metadata.Protocol = "tunnel"
	metadata.Destination = c.tunnel
	c.tunnelNat.NewPacketDirect(ctx, metadata.Source.AddrPort(), conn, buffer, metadata)
	return nil
}

func run(cmd *cobra.Command, flags *Flags) {
	c, err := newClient(flags)
	if err != nil {
		logrus.StandardLogger().Log(logrus.FatalLevel, err, "\n\n")
		cmd.Help()
		os.Exit(1)
	}
	err = c.Start()
	if err != nil {
		logrus.Fatal(err)
	}

	if c.mixIn != nil {
		logrus.Info("mixed server started at ", c.mixIn.TCPListener.Addr())
	} else {
		logrus.Info("tunnel started at ", c.tcpIn.Addr())
	}

	osSignals := make(chan os.Signal, 1)
	signal.Notify(osSignals, os.Interrupt, syscall.SIGTERM)
	<-osSignals

	c.Close()
}

func (c *Client) HandleError(err error) {
	common.Close(err)
	if E.IsClosed(err) {
		return
	}
	logrus.Warn(err)
}
