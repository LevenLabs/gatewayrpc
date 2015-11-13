package main

import (
	"github.com/gorilla/rpc/v2"
	"github.com/gorilla/rpc/v2/json2"
	"github.com/levenlabs/gatewayrpc"
	"github.com/levenlabs/go-llog"
	"github.com/mediocregopher/lever"
	"net/http"
)

var listenAddr string
var remoteAddr string

func main() {
	l := lever.New("server", nil)
	l.Add(lever.Param{
		Name:        "--listen-addr",
		Description: "address:port to listen for rpc request on, or just :port",
		Default:     ":8886",
	})
	l.Add(lever.Param{
		Name:        "--remote-addr",
		Description: "address:port to pull rpc methods from",
		Default:     "127.0.0.1:8887",
	})
	l.Parse()
	listenAddr, _ = l.ParamStr("--listen-addr")
	remoteAddr, _ = l.ParamStr("--remote-addr")

	llog.SetLevelFromString("debug")

	s := gatewayrpc.NewGateway(*rpc.NewServer())
	s.RegisterCodec(json2.NewCodec(), "application/json")
	// empty string means to register all
	s.RegisterRemoteServices(remoteAddr, "")

	err := http.ListenAndServe(listenAddr, s)
	panic(err)
}
