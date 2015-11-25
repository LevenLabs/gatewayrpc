// Package gateway implements the actual gateway which will listen for requests
// and forward them to servers wrapped by gatewayrpc
package gateway

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/gorilla/rpc/v2"
	"github.com/levenlabs/gatewayrpc"
	"github.com/levenlabs/go-llog"
	"github.com/levenlabs/go-srvclient"
	"github.com/levenlabs/golib/rpcutil"
)

type remoteService struct {
	gatewayrpc.Service
	*url.URL
	srv bool
}

// Request contains all the data about an incoming request which is currently
// known. It can be used in conjunction to RequestCallback in order to provide
// extra functionality or checks to request coming through.
//
// Notably, rpc.CodecRequest can be used to read in the arguments of the call
// and write back responses or errors to the client if no forwarding should be
// done.
//
// Also, the Body of the *http.Request will be filled with the raw body of
// content which originally came in, and there's no need to actually close it.
// the *http.Request's URL field will also have been modified to reflect the url
// the request will be being forwarded to.
type Request struct {
	http.ResponseWriter
	*http.Request
	ServiceName string
	gatewayrpc.Method
	rpc.CodecRequest
}

// Gateway is an http.Handler which implements the JSON RPC2 spec, but forwards
// all of its requests onto backend services
type Gateway struct {
	services map[string]remoteService
	mutex    sync.RWMutex
	codecs   map[string]rpc.Codec

	// BackupHandler, if not nil, will be used to handle the requests which
	// don't have a corresponding backend service to forward to (based on their
	// method)
	BackupHandler http.Handler

	// RequestCallback, if not nil, will be called just before actually
	// forwarding a request onto its backend service. If false is returned, the
	// forwarding will not be done. See the Request docstring for more on what
	// is actually possible with this
	RequestCallback func(Request) bool
}

// NewGateway returns an instantiated Gateway object
func NewGateway() Gateway {
	return Gateway{
		services: map[string]remoteService{},
		codecs:   map[string]rpc.Codec{},
	}
}

// returns a copy of the given url, with the host potentially resolved using a
// srv request
func resolveURL(uu *url.URL) (*url.URL, bool) {
	var srv bool
	uu2 := *uu
	host, err := srvclient.SRV(uu2.Host)
	if err == nil {
		srv = true
		uu2.Host = host
	}
	return &uu2, srv
}

// AddURL performs the RPC.GetServices request against the given url, and will
// add all returned services to its mapping.
//
// All DNS will be attempted to be resolved using SRV records first, and will
// use a normal DNS request as a backup
func (g Gateway) AddURL(u string) error {
	uu, err := url.Parse(u)
	if err != nil {
		return err
	}

	uu2, srvOK := resolveURL(uu)

	res := gatewayrpc.GetServicesRes{}
	if err = rpcutil.JSONRPC2Call(uu2.String(), &res, "RPC.GetServices", &struct{}{}); err != nil {
		return err
	}

	g.mutex.Lock()
	defer g.mutex.Unlock()
	for _, srv := range res.Services {
		g.services[srv.Name] = remoteService{
			Service: srv,
			URL:     uu,
			srv:     srvOK,
		}
	}
	return nil
}

// RegisterCodec is used to register an encoder/decoder which will operate on
// requests with the given contentType
func (g Gateway) RegisterCodec(codec rpc.Codec, contentType string) {
	g.codecs[strings.ToLower(contentType)] = codec
}

// returns the string form of the url which should handle this method
// ("Service.MethodName"), as well as the method's details. Returns an error if
// the method couldn't be found
func (g Gateway) getMethod(mStr string) (*url.URL, string, gatewayrpc.Method, error) {
	var m gatewayrpc.Method

	parts := strings.SplitN(mStr, ".", 2)
	if len(parts) != 2 {
		return nil, "", m, errors.New("invalid method endpoint given")
	}
	srvName, mName := parts[0], parts[1]

	g.mutex.RLock()
	rsrv, ok := g.services[srvName]
	g.mutex.RUnlock()
	if !ok {
		return nil, srvName, m, errors.New("unknown service name")
	}

	m, ok = rsrv.Methods[mName]
	if !ok {
		return nil, srvName, m, errors.New("unknown method name")
	}

	if rsrv.srv {
		uu, srvOK := resolveURL(rsrv.URL)
		if !srvOK {
			return nil, srvName, m, errors.New("could not resolve host via srv")
		}
		return uu, srvName, m, nil
	}

	return rsrv.URL, srvName, m, nil
}

// GetMethodURL returns the url which should be used to call the given method
// ("Service.MethodName"). If the service was originally resolved using a srv
// request it will be re-resolved everytime this is called, in order to
// load-balance across instances. Will return an error if the service is
// unknown, or the resolving fails for some reason.
func (g Gateway) GetMethodURL(mStr string) (*url.URL, error) {
	u, _, _, err := g.getMethod(mStr)
	return u, err
}

// We really only need the params part of this, we can get everything else from
// the codec
type serverRequest struct {
	// A Structured value to pass as arguments to the method.
	Params *json.RawMessage `json:"params"`
}

func (g Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	kv := rpcutil.RequestKV(r)
	llog.Debug("ServeHTTP called", kv)

	// except for the actually calling/validation this is just copy/paste from gorilla/rpc/v2
	if r.Method != "POST" {
		kv["method"] = r.Method
		llog.Warn("Invalid method sent", kv)
		writeErrorf(w, 405, "rpc: POST method required, received %q", r.Method)
		return
	}

	contentType := r.Header.Get("Content-Type")
	idx := strings.Index(contentType, ";")
	if idx != -1 {
		contentType = contentType[:idx]
	}
	codec := g.codecs[strings.ToLower(contentType)]
	if codec == nil {
		kv["contentType"] = contentType
		llog.Warn("Unrecognized Content-Type", kv)
		writeErrorf(w, 415, "rpc: unrecognized Content-Type: %q", contentType)
		return
	}

	bodyCopy := bytes.NewBuffer(make([]byte, 0, r.ContentLength))
	r.Body = ioutil.NopCloser(io.TeeReader(r.Body, bodyCopy))
	codecReq := codec.NewRequest(r)
	r.Body = ioutil.NopCloser(bodyCopy)

	// At this point the original r.Body was read into the codecReq, but the
	// TeeReader has also copied it into bodyCopy, which was then set to r.Body

	m, err := codecReq.Method()
	if err != nil {
		kv["err"] = err
		llog.Warn("Err retrieving method from codec", kv)
		codecReq.WriteError(w, 400, err)
		return
	}

	kv["method"] = m
	llog.Debug("Received method call", kv)

	u, rpcSrvName, rpcMethod, err := g.getMethod(m)
	if err != nil {
		if g.BackupHandler != nil {
			g.BackupHandler.ServeHTTP(w, r)
			return
		}
		kv["err"] = err
		llog.Warn("Error retrieving method from gatway", kv)
		codecReq.WriteError(w, 400, err)
	}
	r.URL = u
	r.RequestURI = ""

	if g.RequestCallback != nil && !g.RequestCallback(Request{
		ResponseWriter: w,
		Request:        r,
		ServiceName:    rpcSrvName,
		Method:         rpcMethod,
		CodecRequest:   codecReq,
	}) {
		return
	}

	res, err := http.DefaultClient.Do(r)
	if err != nil {
		kv["err"] = err
		llog.Error("Error forwarding request", kv)
		codecReq.WriteError(w, 500, err)
		return
	}
	defer res.Body.Close()

	//pass along the content-type
	w.Header().Set("Content-Type", contentType)
	io.Copy(w, res.Body)
	res.Body.Close()
}

func writeErrorf(w http.ResponseWriter, status int, msg string, args ...interface{}) {
	w.WriteHeader(status)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprint(w, fmt.Sprintf(msg, args...))
}
