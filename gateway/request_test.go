package gateway

import (
	"bytes"
	"github.com/gorilla/rpc/v2/json2"
	"github.com/levenlabs/golib/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	. "testing"
)

func getFooRequest() (*Request, FooArgs, error) {
	args := FooArgs{
		A: testutil.RandInt64(),
		B: testutil.RandStr(),
	}
	req := &Request{
		respWriter: httptest.NewRecorder(),
	}
	b, err := json2.EncodeClientRequest("Test.Test", &args)
	if err != nil {
		return req, args, err
	}
	if req.Request, err = http.NewRequest("POST", "http://127.0.0.1", bytes.NewBuffer(b)); err != nil {
		return req, args, err
	}
	req.codecReq = json2.NewCodec().NewRequest(req.Request)
	return req, args, err
}

func TestReadRequest(t *T) {
	r, args, err := getFooRequest()
	require.Nil(t, err)

	args2 := FooArgs{}
	err = r.ReadRequest(&args2)
	require.Nil(t, err)

	assert.Equal(t, args, args2)
}

func TestUpdateRequest(t *T) {
	r, _, err := getFooRequest()
	require.Nil(t, err)

	args := FooArgs{}
	err = r.ReadRequest(&args)
	require.Nil(t, err)

	args.A = testutil.RandInt64()
	args.B = testutil.RandStr()
	err = r.UpdateRequest("", &args)
	require.Nil(t, err)

	args2 := FooArgs{}
	err = r.ReadRequest(&args2)
	require.Nil(t, err)

	assert.Equal(t, args, args2)
}

func TestMethod(t *T) {
	r, _, err := getFooRequest()
	require.Nil(t, err)

	m, err := r.Method()
	require.Nil(t, err)

	assert.Equal(t, "Test.Test", m)

	err = r.UpdateRequest("Test.Test2", nil)
	require.Nil(t, err)

	m, err = r.Method()
	require.Nil(t, err)

	assert.Equal(t, "Test.Test2", m)
}

func equalRequest(t *T, b []byte, m string, args FooArgs) {
	req, err := http.NewRequest("POST", "http://127.0.0.1", bytes.NewBuffer(b))
	require.Nil(t, err)
	cReq := json2.NewCodec().NewRequest(req)

	m2, err := cReq.Method()
	assert.Nil(t, err)
	assert.Equal(t, m, m2)

	args2 := FooArgs{}
	cReq.ReadRequest(&args2)
	assert.Nil(t, err)
	assert.Equal(t, args, args2)
}

func TestClientRequest(t *T) {
	r, args, err := getFooRequest()
	require.Nil(t, err)

	b, err := r.getClientRequest()
	require.Nil(t, err)

	equalRequest(t, b, "Test.Test", args)

	args.A = testutil.RandInt64()
	args.B = testutil.RandStr()
	err = r.UpdateRequest("Test.Test2", args)
	require.Nil(t, err)

	b, err = r.getClientRequest()
	require.Nil(t, err)

	equalRequest(t, b, "Test.Test2", args)
}
