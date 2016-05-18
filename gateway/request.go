package gateway

import (
	"encoding/json"
	"github.com/gorilla/rpc/v2"
	"github.com/gorilla/rpc/v2/json2"
	"github.com/levenlabs/gatewayrpc/gatewaytypes"
	"net/http"
)

// Request contains all the data about an incoming request which is currently
// known. It can be used in conjunction to RequestCallback in order to provide
// extra functionality or checks to request coming through.
//
// It provides methods to write an error, response, get the method and get the
// args in a map[string]interface{} that can be modified in order to change the
// request before it gets sent to the handler
//
// Although the CodecRequest is public, it's use is deprecated. Also, the
// ResponseWriter should not be used and instead use Write* methods on Request.
type Request struct {
	*http.Request
	RemoteMethod gatewaytypes.Method
	ServiceName  string

	respWriter http.ResponseWriter
	codecReq   rpc.CodecRequest
	newMethod  string
	args       json.RawMessage
	responded  bool
}

// Method returns the RPC method that this request is going to call
func (r *Request) Method() (string, error) {
	if r.newMethod != "" {
		return r.newMethod, nil
	}
	return r.codecReq.Method()
}

// WriteError responds to the client with an error code and error it deals with
// the CodecRequest so you don't have to After calling, you should return false
// from the callback
func (r *Request) WriteError(status int, err error) {
	r.responded = true
	r.codecReq.WriteError(r.respWriter, status, err)
}

// WriteResponse responds to the client with the sent result
// After calling, you should return false from the callback
func (r *Request) WriteResponse(i interface{}) {
	r.responded = true
	r.codecReq.WriteResponse(r.respWriter, i)
}

// ReadRequest fills in the args into the passed interface
// If you change the struct you passed, you must call UpdateRequest and pass
// the updated struct in order to actually affect the forwarded request
func (r *Request) ReadRequest(v interface{}) error {
	if len(r.args) > 0 {
		return json.Unmarshal(r.args, v)
	}
	return r.codecReq.ReadRequest(v)
}

// UpdateRequest takes a new method string and an interface that it json
// encodes to new params for the request. If method is empty then the method will
// not be changed. If params is nil, then params will not be changed.
func (r *Request) UpdateRequest(method string, params interface{}) error {
	var err error
	if method != "" {
		r.newMethod = method
	}
	if params != nil {
		var a []byte
		if a, err = json.Marshal(params); err != nil {
			return err
		}
		err = r.args.UnmarshalJSON(a)
	}
	return err
}

func (r *Request) getClientRequest() ([]byte, error) {
	var err error
	if len(r.args) == 0 {
		if err = r.codecReq.ReadRequest(&r.args); err != nil {
			return nil, err
		}
	}
	m, err := r.Method()
	if err != nil {
		return nil, err
	}
	return json2.EncodeClientRequest(m, &r.args)
}
