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
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/rpc/v2"
	"github.com/gorilla/rpc/v2/json2"
	"github.com/levenlabs/gatewayrpc/gatewaytypes"
	"github.com/levenlabs/go-llog"
	"github.com/levenlabs/go-srvclient"
	"github.com/levenlabs/golib/rpcutil"
)

type remoteService struct {
	gatewaytypes.Service
	*url.URL
	origURL string
}

var externalHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	res, err := http.DefaultClient.Do(r)
	if err != nil {
		llog.Error("error forwarding request", llog.KV{
			"url": r.URL.String(),
			"err": err,
		})
		writeErrorf(w, 500, "{}")
		return
	}
	defer res.Body.Close()

	//pass along the content-type
	w.Header().Set("Content-Type", res.Header.Get("Content-Type"))
	io.Copy(w, res.Body)
})

// Gateway is an http.Handler which implements the JSON RPC2 spec, but forwards
// all of its requests onto backend services
type Gateway struct {
	services map[string]remoteService
	mutex    sync.RWMutex
	codecs   map[string]rpc.Codec
	poll     <-chan time.Time
	srv      *srvclient.SRVClient

	// BackupHandler, if not nil, will be used to handle the requests which
	// don't have a corresponding backend service to forward to (based on their
	// method)
	BackupHandler http.Handler

	// RequestCallback, if not nil, will be called just before actually
	// forwarding a request onto its backend service. See the Request docstring
	// for more on what is actually possible with this. If you respond to the
	// Request using a Write* method then no forwarding will be done
	RequestCallback func(*Request)

	// CORSMatch, if not nil, will be used against the Origin header. and if it
	// matches Access-Control-Allow-* headers will be sent back, including an
	// Allow-Access-Control-Origin matching the sent in Origin
	CORSMatch *regexp.Regexp
}

// NewGateway returns an instantiated Gateway object
func NewGateway() Gateway {
	srv := &srvclient.SRVClient{}
	srv.EnableCacheLast()
	return Gateway{
		services: map[string]remoteService{},
		codecs:   map[string]rpc.Codec{},
		poll:     time.Tick(30 * time.Second),
		srv:      srv,
	}
}

// returns a copy of the given url, with the host potentially resolved using a
// srv request
func (g Gateway) resolveURL(uu *url.URL) *url.URL {
	uu2 := *uu
	uu2.Host = g.srv.MaybeSRV(uu.Host)
	return &uu2
}

// AddURL performs the RPC.GetServices request against the given url, and will
// add all returned services to its mapping.
//
// All DNS will be attempted to be resolved using SRV records first, and will
// use a normal DNS request as a backup
func (g Gateway) AddURL(u string) error {
	if !strings.HasPrefix(u, "http") {
		u = "http://" + u
	}
	uu, err := url.Parse(u)
	if err != nil {
		return err
	}
	if uu.Host == "" {
		return errors.New("invalid url specified")
	}

	u2 := g.resolveURL(uu).String()
	llog.Debug("resolved add url", llog.KV{"originalURL": u, "resolvedURL": u2})

	res := struct {
		Services []gatewaytypes.Service `json:"services"`
	}{}
	if err = rpcutil.JSONRPC2Call(u2, &res, "RPC.GetServices", &struct{}{}); err != nil {
		return err
	}

	for _, srv := range res.Services {
		for m := range srv.Methods {
			llog.Debug("adding method", llog.KV{"service": srv.Name, "method": m})
		}
	}

	g.mutex.Lock()
	defer g.mutex.Unlock()
	for _, srv := range res.Services {
		g.services[srv.Name] = remoteService{
			Service: srv,
			URL:     uu,
			origURL: u,
		}
	}
	return nil
}

func (g Gateway) refreshURLs() {
	llog.Debug("refreshing urls")
	g.mutex.RLock()
	srvs := make([]remoteService, 0, len(g.services))
	for _, srv := range g.services {
		srvs = append(srvs, srv)
	}
	g.mutex.RUnlock()

	for _, srv := range srvs {
		if err := g.AddURL(srv.origURL); err != nil {
			llog.Error("error refreshing url", llog.KV{
				"url": srv.origURL,
				"err": err,
			})
		}
	}
}

// RegisterCodec is used to register an encoder/decoder which will operate on
// requests with the given contentType
func (g Gateway) RegisterCodec(codec rpc.Codec, contentType string) {
	g.codecs[strings.ToLower(contentType)] = codec
}

func (g Gateway) getMethod(mStr string) (rsrv remoteService, m gatewaytypes.Method, err error) {
	parts := strings.SplitN(mStr, ".", 2)
	if len(parts) != 2 {
		err = errors.New("invalid method endpoint given")
		return
	}
	srvName, mName := parts[0], parts[1]

	var ok bool
	g.mutex.RLock()
	defer g.mutex.RUnlock()
	if rsrv, ok = g.services[srvName]; !ok {
		err = errors.New("no remote service for given name")
	} else if m, ok = rsrv.Methods[mName]; !ok {
		err = errors.New("remote service cannot handle this method")
	}
	return
}

// GetMethodURL returns the url which should be used to call the given method
// ("Service.MethodName"). If the service was originally resolved using a srv
// request it will be re-resolved everytime this is called, in order to
// load-balance across instances. Will return an error if the service is
// unknown, or the resolving fails for some reason.
func (g Gateway) GetMethodURL(mStr string) (*url.URL, error) {
	rsrv, _, err := g.getMethod(mStr)
	return g.resolveURL(rsrv.URL), err
}

// We really only need the params part of this, we can get everything else from
// the codec
type serverRequest struct {
	// A Structured value to pass as arguments to the method.
	Params *json.RawMessage `json:"params"`
}

// ServeHTTP satisfies Gateway being a http.Handler
func (g Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Periodically we want to refresh the services that gateway knows about. We
	// do it in a new goroutine so we don't block this actual request. We don't
	// want to simply have a dedicated go routine looping over the poll channel
	// to do this because having an http.Handler spawn up its own routine that's
	// making requests and doing stuff is kind of unexpected behavior
	select {
	case <-g.poll:
		go g.refreshURLs()
	default:
	}

	kv := rpcutil.RequestKV(r)
	llog.Debug("ServeHTTP called", kv)

	// Possibly check CORS and set the headers to send back if it matches
	origin := r.Header.Get("Origin")
	if origin != "" && g.CORSMatch != nil && g.CORSMatch.MatchString(origin) {
		w.Header().Add("Access-Control-Allow-Origin", origin)
		w.Header().Add("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Add("Access-Control-Allow-Credentials", "true")
		w.Header().Add("Access-Control-Allow-Headers", "DNT, User-Agent, X-Requested-With, Content-Type")
	}

	// We allow OPTIONS so that preflighted requests can get CORS back
	if r.Method == "OPTIONS" {
		return
	}

	if r.Method != "POST" {
		kv["method"] = r.Method
		llog.Warn("invalid method sent", kv)
		writeErrorf(w, 405, "rpc: POST method required, received %q", r.Method)
		return
	}

	contentType := r.Header.Get("Content-Type")
	idx := strings.Index(contentType, ";")
	if idx != -1 {
		contentType = contentType[:idx]
	}
	var codec rpc.Codec
	// if no contentType was sent, assume the first codec if only one in list
	// see: https://github.com/gorilla/rpc/pull/42/
	if contentType == "" && len(g.codecs) == 1 {
		// since codecs is a map we just need to loop and stop after the first
		for _, c := range g.codecs {
			codec = c
			break
		}
	} else if codec = g.codecs[strings.ToLower(contentType)]; codec == nil {
		kv["contentType"] = contentType
		llog.Warn("unknown content-type sent", kv)
		writeErrorf(w, 415, "rpc: unrecognized Content-Type: %q", contentType)
		return
	}
	// note: this will consume the r.Body
	codecReq := codec.NewRequest(r)

	m, err := codecReq.Method()
	if err != nil {
		kv["err"] = err
		llog.Warn("error retrieving method from codec", kv)
		codecReq.WriteError(w, 400, err)
		return
	}

	kv["method"] = m
	llog.Debug("Received method call", kv)

	var handler http.Handler
	rsrv, rpcMethod, err := g.getMethod(m)
	if err != nil {
		// if they passed a backup handler then use that instead of erroring
		if g.BackupHandler != nil {
			handler = g.BackupHandler
		} else {
			kv["err"] = err
			llog.Warn("error getting method in gateway", kv)
			codecReq.WriteError(w, 400, err)
			return
		}
	} else {
		// if there wasn't an error then we found an appropriate remote
		handler = externalHandler
	}

	req := &Request{
		Request:      r,
		ServiceName:  rsrv.Name,
		RemoteMethod: rpcMethod,
		respWriter:   w,
		codecReq:     codecReq,
	}
	// resolve the url so we can forward it, if this is a remote request
	if rsrv.URL != nil {
		r.URL = g.resolveURL(rsrv.URL)
	} else {
		// this must be a request going to BackupHandler
		r.URL = nil
	}
	r.RequestURI = ""

	if g.RequestCallback != nil {
		g.RequestCallback(req)
	}

	// if something already responded to the request inside the callback, don't
	// continue
	if req.responded {
		return
	}

	// make a new request to send to the backend since the request
	// might've been changed
	// also when we called codec.NewRequest earlier that read r.Body
	// so we no longer have the original body
	b, err := req.getClientRequest()
	if err != nil {
		kv["err"] = err
		llog.Warn("error encoding request to remote service", kv)
		codecReq.WriteError(w, 500, err)
		return
	}
	r.Body = ioutil.NopCloser(bytes.NewBuffer(b))
	// since we overwrote the body, we need to update Content-Length
	r.ContentLength = int64(len(b))
	rec := httptest.NewRecorder()
	// since we wrote a new client request, we need to buffer the response
	// and rewrite it using our original codec request
	handler.ServeHTTP(rec, r)

	// we don't actually care what the response was so just use a RawMessage
	resRes := &json.RawMessage{}
	if err = json2.DecodeClientResponse(rec.Body, resRes); err != nil {
		codecReq.WriteError(w, rec.Code, err)
	} else {
		codecReq.WriteResponse(w, resRes)
	}
}

func writeErrorf(w http.ResponseWriter, status int, msg string, args ...interface{}) {
	w.WriteHeader(status)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprint(w, fmt.Sprintf(msg, args...))
}
