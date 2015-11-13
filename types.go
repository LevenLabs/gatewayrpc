package gatewayrpc

import (
	"github.com/gorilla/rpc/v2"
	"net/http"
)

type Server interface {
	RegisterCodec(codec rpc.Codec, contentType string)

	RegisterService(receiver interface{}, name string) error

	HasMethod(method string) bool

	ServeHTTP(w http.ResponseWriter, r *http.Request)

	WriteError(w http.ResponseWriter, status int, msg string)
}

type Service struct {
	Name     string `json:"name"`
	receiver interface{}
	Methods  map[string]*Method `json:"methods"`
}

type Method struct {
	Name   string `json:"name"`
	Args   []*Arg `json:"args"`
	Return *Arg   `json:"return"`
}

type Arg struct {
	//todo: something like a ReflectValue or whatever describing the type
}
