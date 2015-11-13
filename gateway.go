package gatewayrpc

import (
	"bytes"
	"fmt"
	"github.com/gorilla/rpc/v2"
	"github.com/gorilla/rpc/v2/json2"
	"github.com/levenlabs/go-llog"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

type ServiceList struct {
	Services map[string]*RemoteService
	mutex    sync.RWMutex
}

func (s *ServiceList) get(servMethod string) (*RemoteService, *Method, error) {
	p := strings.Split(servMethod, ".")
	if len(p) != 2 {
		err := fmt.Errorf("rpc: service/method request ill-formed: %q", servMethod)
		return nil, nil, err
	}
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	serv, ok := s.Services[p[0]]
	if !ok {
		err := fmt.Errorf("rpc: can't find service %q", servMethod)
		return nil, nil, err
	}
	m, ok := serv.Methods[p[1]]
	if !ok {
		err := fmt.Errorf("rpc: can't find method %q", servMethod)
		return nil, nil, err
	}
	return serv, m, nil
}

func (s *ServiceList) add(services []*RemoteService, overwrite bool) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	// first verify that none already exist
	if overwrite {
		for _, v := range services {
			if _, ok := s.Services[v.Name]; ok {
				return fmt.Errorf("Service already exists: %s", v.Name)
			}
		}
	}
	if s.Services == nil {
		s.Services = make(map[string]*RemoteService)
	}
	// now actually add them
	for _, v := range services {
		s.Services[v.Name] = v
	}
	return nil
}

func makeRemoteRequest(urlString string, payload []byte, contentType string) ([]byte, error) {
	req, err := http.NewRequest("POST", urlString, bytes.NewBuffer(payload))
	if err != nil {
		return nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	c := &http.Client{}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("rpc: non-200 status code returned from %s: %i", urlString, resp.Status)
	}
	return ioutil.ReadAll(resp.Body)
}

type RemoteService struct {
	Service
	url string
}

func (s *RemoteService) Call(method string, req []byte, contentType string) ([]byte, error) {
	// we don't need a mutex here since Methods map should NEVER change after creation
	_, ok := s.Methods[method]
	if !ok {
		return nil, fmt.Errorf("rpc: can't find method %q on service %s", method, s.Name)
	}
	llog.Info("Calling remote endpoint", llog.KV{"method": method, "url": s.url})
	//todo: validate args
	return makeRemoteRequest(s.url, req, contentType)
}

type GatewayServer struct {
	rpc.Server
	codecs         map[string]rpc.Codec
	remoteServices ServiceList
}

func NewGateway(enc rpc.Server) *GatewayServer {
	return &GatewayServer{enc, map[string]rpc.Codec{}, ServiceList{}}
}

func (s *GatewayServer) HasMethod(method string) bool {
	has := s.Server.HasMethod(method)
	if !has {
		_, _, err := s.remoteServices.get(method)
		has = err == nil
	}
	return has
}

func getRemoteServices(address string, name string) ([]*RemoteService, error) {
	//todo: don't assume JSON?
	b, err := json2.EncodeClientRequest("RPC.GetMethods", GetMethodsArgs{name})
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(address)
	if err != nil {
		return nil, err
	}
	if u.Scheme == "" {
		u.Scheme = "http"
	}
	if u.Path == "" {
		u.Path = "rpc"
	}

	ustr := u.String()
	res, err := makeRemoteRequest(ustr, b, "application/json; charset=utf-8")
	if err != nil {
		llog.Error("Error making remote request", llog.KV{"url": ustr})
		return nil, err
	}
	reply := &GetMethodsReply{}
	err = json2.DecodeClientResponse(bytes.NewBuffer(res), reply)
	if err != nil {
		llog.Error("Error decoding remote response", llog.KV{"error": err})
		return nil, err
	}
	services := make([]*RemoteService, len(reply.Services))
	for i, v := range reply.Services {
		services[i] = &RemoteService{*v, ustr}
	}
	return services, nil
}

func (s *GatewayServer) importServices(serv []*RemoteService, overwrite bool) error {
	return s.remoteServices.add(serv, overwrite)
}

// Remote endpoint MUST support the RPC.GetMethods call over application/json
func (s *GatewayServer) RegisterRemoteServices(address, name string) error {
	kv := llog.KV{"address": address, "name": name}
	llog.Info("Registering services from remote", kv)
	services, err := getRemoteServices(address, name)
	if err != nil {
		kv["error"] = err
		llog.Error("Error importing services", kv)
		return err
	}
	llog.Debug("Found services from remote", llog.KV{"num": len(services)})
	return s.importServices(services, false)
	//todo: start go routine to refresh the list of services
}

func (s *GatewayServer) RegisterCodec(codec rpc.Codec, contentType string) {
	// since codecs is lowercase we can't just rely on the underlying server
	// to handle this
	s.codecs[strings.ToLower(contentType)] = codec
	// since we're falling back on the other server though we need to add it still
	s.Server.RegisterCodec(codec, contentType)
}

func (s *GatewayServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	llog.Debug("ServeHTTP called")
	// except for the actually calling/validation this is just copy/paste from gorilla/rpc/v2
	if r.Method != "POST" {
		llog.Warn("Invalid method sent", llog.KV{"method": r.Method})
		WriteError(w, 405, "rpc: POST method required, received "+r.Method)
		return
	}
	contentType := r.Header.Get("Content-Type")
	idx := strings.Index(contentType, ";")
	if idx != -1 {
		contentType = contentType[:idx]
	}
	codec := s.codecs[strings.ToLower(contentType)]
	if codec == nil {
		llog.Warn("Unrecognized Content-Type", llog.KV{"type": contentType})
		WriteError(w, 415, "rpc: unrecognized Content-Type: "+contentType)
		return
	}
	body, _ := ioutil.ReadAll(r.Body)
	r.Body.Close()
	// now we need to convert body back into a new buffer for NewRequest
	r.Body = ioutil.NopCloser(bytes.NewBuffer(body))
	// Create a new codec request.
	codecReq := codec.NewRequest(r)
	// Get service method to be called.
	method, err := codecReq.Method()
	if err != nil {
		llog.Warn("Method not sent", llog.KV{"error": err})
		codecReq.WriteError(w, 400, err)
		return
	}
	llog.Info("Received method call", llog.KV{"method": method})
	serv, m, err := s.remoteServices.get(method)
	if err != nil {
		llog.Info("Method not found in remote services", llog.KV{"method": method})
		// if it isn't a remote service, punt it over to backing ServeHTTP
		s.Server.ServeHTTP(w, r)
		return
	}

	var res []byte
	res, err = serv.Call(m.Name, body, contentType)
	if err != nil {
		llog.Info("Error calling remote method", llog.KV{"error": err})
		codecReq.WriteError(w, 400, err)
		return
	}
	//pass along the content-type
	w.Header().Set("Content-Type", contentType)
	w.Write(res)
}

func WriteError(w http.ResponseWriter, status int, msg string) {
	w.WriteHeader(status)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprint(w, msg)
}
