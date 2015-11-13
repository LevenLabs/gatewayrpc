package main

import (
	"fmt"
	"github.com/gorilla/rpc/v2"
	"github.com/gorilla/rpc/v2/json2"
	"github.com/levenlabs/gatewayrpc"
	"github.com/levenlabs/go-llog"
	"github.com/mediocregopher/lever"
	"net/http"
)

var listenAddr string

type Math struct{}

func (m *Math) Add(w *http.Request, args *[]int64, r *int64) error {
	sum := int64(0)
	llog.Debug("Got Add call", llog.KV{"args": fmt.Sprintf("%+v", args)})
	for _, v := range *args {
		sum += v
	}
	llog.Debug("Sum", llog.KV{"sum": sum})
	*r = sum
	return nil
}

func main() {
	l := lever.New("server", nil)
	l.Add(lever.Param{
		Name:        "--listen-addr",
		Description: "address:port to listen for rpc request on, or just :port",
		Default:     ":8887",
	})
	l.Parse()
	listenAddr, _ = l.ParamStr("--listen-addr")

	llog.SetLevelFromString("debug")

	s := gatewayrpc.NewServer(*rpc.NewServer())
	s.RegisterCodec(json2.NewCodec(), "application/json")
	s.RegisterService(&Math{}, "")

	err := http.ListenAndServe(listenAddr, s)
	panic(err)
}
