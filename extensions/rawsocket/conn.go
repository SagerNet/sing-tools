package rawsocket

import (
	"context"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sagernet/sing/common/buf"
	"github.com/sagernet/sing/common/bufio"
)

func DialContext(ctx context.Context, urlStr string, requestHeader http.Header) (net.Conn, *http.Response, error) {
	var dialer websocket.Dialer
	conn, response, err := dialer.DialContext(ctx, urlStr, requestHeader)
	if err != nil {
		return nil, response, err
	}
	var wsConn websocketConn
	wsConn.Conn = conn
	wsConn.readBuffer = buf.New()
	wsConn.reader = bufio.NewBufferedReader(&wsConn.rawWebsocketConn, wsConn.readBuffer)
	return &wsConn, response, nil
}

func Upgrade(w http.ResponseWriter, r *http.Request, responseHeader http.Header) (net.Conn, error) {
	var upgrader websocket.Upgrader
	conn, err := upgrader.Upgrade(w, r, responseHeader)
	if err != nil {
		return nil, err
	}
	var wsConn websocketConn
	wsConn.Conn = conn
	wsConn.readBuffer = buf.New()
	wsConn.reader = bufio.NewBufferedReader(&wsConn.rawWebsocketConn, wsConn.readBuffer)
	return &wsConn, nil
}

type websocketConn struct {
	rawWebsocketConn
	readBuffer *buf.Buffer
	reader     *bufio.BufferedReader
}

func (c *websocketConn) Read(p []byte) (n int, err error) {
	return c.reader.Read(p)
}

func (c *websocketConn) Close() error {
	c.readBuffer.Release()
	return c.Conn.Close()
}

type rawWebsocketConn struct {
	*websocket.Conn
}

func (c *rawWebsocketConn) Read(b []byte) (n int, err error) {
	_, p, err := c.ReadMessage()
	if err != nil {
		return
	}
	if len(b) < len(p) {
		return 0, io.ErrShortBuffer
	}
	n = copy(b, p)
	return
}

func (c *rawWebsocketConn) Write(b []byte) (n int, err error) {
	err = c.WriteMessage(websocket.BinaryMessage, b)
	if err != nil {
		return
	}
	n = len(b)
	return
}

func (c *rawWebsocketConn) SetDeadline(t time.Time) error {
	err := c.SetReadDeadline(t)
	if err != nil {
		return err
	}
	err = c.SetWriteDeadline(t)
	if err != nil {
		return err
	}
	return nil
}
