package gateway

import (
	"errors"
	"net/http"
	"net/http/httptest"
	. "testing"

	"github.com/gorilla/rpc/v2"
	"github.com/gorilla/rpc/v2/json2"
	"github.com/levenlabs/gatewayrpc"
	"github.com/levenlabs/golib/rpcutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type TestEndpoint struct{}

type FooArgs struct {
	A int64  `json:"a"`
	B string `json:"b"`
}

type FooRes struct {
	FooArgs `json:"args"`
}

func (t TestEndpoint) Foo(r *http.Request, args *FooArgs, res *FooRes) error {
	res.FooArgs = *args
	return nil
}

type BarArgs struct {
	A int                    `json:"a"`
	B []int                  `json:"b"`
	C []FooArgs              `json:"c"`
	D map[string]interface{} `json:"d"`
}

func (t TestEndpoint) Bar(r *http.Request, args *BarArgs, _ *struct{}) error {
	return nil
}

type TestEndpoint2 struct{}

func (t2 TestEndpoint2) Wat(r *http.Request, _ *struct{}, res *struct{ A int }) error {
	res.A = 5
	return nil
}

func init() {
	h := gatewayrpc.NewServer()
	h.RegisterService(TestEndpoint{}, "")
	h.RegisterCodec(json2.NewCodec(), "application/json")
	s := httptest.NewServer(h)
	testURL = s.URL

	testGateway = NewGateway()
	testGateway.RegisterCodec(json2.NewCodec(), "application/json")
	if err := testGateway.AddURL(testURL); err != nil {
		panic(err)
	}

	testGateway.RequestCallback = func(r *Request) {
		if m, _ := r.Method(); m != "TestEndpoint.Bar" {
			return
		}

		args := BarArgs{}
		if err := r.ReadRequest(&args); err != nil {
			r.WriteError(400, errors.New("couldn't read args"))
			return
		}

		if args.A == 5 {
			r.WriteResponse(map[string]bool{"Success": true})
			return
		}
	}

	backupHandler := rpc.NewServer()
	backupHandler.RegisterCodec(json2.NewCodec(), "application/json")
	backupHandler.RegisterService(TestEndpoint2{}, "")
	testGateway.BackupHandler = backupHandler
}

var testURL string
var testGateway *Gateway

func TestGetMethod(t *T) {
	// testGateway already had AddURL called on it, so we just check that the
	// data is there
	rsrv, m, err := testGateway.getMethod("TestEndpoint.Foo")
	require.Nil(t, err)
	assert.Equal(t, testURL, rsrv.URL.String())
	assert.Equal(t, "Foo", m.Name)

	u, err := testGateway.GetMethodURL("TestEndpoint.Foo")
	require.Nil(t, err)
	assert.Equal(t, testURL, u.String())
}

func TestForwarding(t *T) {
	args := FooArgs{
		A: 1,
		B: "one",
	}
	var res FooRes
	require.Nil(t, rpcutil.JSONRPC2CallHandler(testGateway, &res, "TestEndpoint.Foo", &args))
	assert.Equal(t, args, res.FooArgs)
}

func TestCallback(t *T) {
	var res struct{ Success bool }
	args := BarArgs{}
	require.Nil(t, rpcutil.JSONRPC2CallHandler(testGateway, &res, "TestEndpoint.Bar", &args))
	assert.False(t, res.Success)

	args.A = 5

	require.Nil(t, rpcutil.JSONRPC2CallHandler(testGateway, &res, "TestEndpoint.Bar", &args))
	assert.True(t, res.Success)
}

func TestBackupHandler(t *T) {
	var res struct{ A int }
	require.Nil(t, rpcutil.JSONRPC2CallHandler(testGateway, &res, "TestEndpoint2.Wat", &struct{}{}))
	assert.Equal(t, 5, res.A)
}
