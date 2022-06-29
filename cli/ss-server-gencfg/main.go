package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"os"

	"github.com/sagernet/sing/common"
)

type Flags struct {
	Server     string `json:"server"`
	ServerPort uint16 `json:"server_port"`
	Password   string `json:"password"`
	Method     string `json:"method"`
	LogLevel   string `json:"log_level"`
}

func main() {
	password := make([]byte, 16)
	common.Must1(io.ReadFull(rand.Reader, password))

	f := new(Flags)
	f.Server = "::"
	f.ServerPort = 8080
	f.Password = base64.StdEncoding.EncodeToString(password)
	f.Method = "2022-blake3-aes-128-gcm"
	f.LogLevel = "warn"

	c, err := json.MarshalIndent(f, "", "  ")
	common.Must(err)
	common.Must1(os.Stdout.Write(c))
}
