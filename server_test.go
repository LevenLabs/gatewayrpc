package gatewayrpc

import (
	"net/http"
	"reflect"
	. "testing"

	"github.com/gorilla/rpc/v2/json2"
	"github.com/levenlabs/golib/rpcutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type TestEndpoint struct{}

type FooArgs struct {
	A int    `json:"a"`
	B string `json:"b"`
}

var fooArgsType = &Type{ObjectOf: map[string]*Type{
	"a": &Type{TypeOf: reflect.Int},
	"b": &Type{TypeOf: reflect.String},
}}

type FooRes struct {
	FooArgs `json:"args"`
}

var fooResType = &Type{ObjectOf: map[string]*Type{
	"args": fooArgsType,
}}

func (t TestEndpoint) Foo(r *http.Request, args *FooArgs, res *FooRes) error {
	res.FooArgs = *args
	return nil
}

type BazArgs struct {
	AA int `json:"aa"`
}

type BarArgs struct {
	A int                    `json:"a"`
	B []int                  `json:"b"`
	C []FooArgs              `json:"c"`
	D map[string]interface{} `json:"d"`
	BazArgs
}

var barArgsType = &Type{ObjectOf: map[string]*Type{
	"a":  &Type{TypeOf: reflect.Int},
	"b":  &Type{ArrayOf: &Type{TypeOf: reflect.Int}},
	"c":  &Type{ArrayOf: fooArgsType},
	"d":  &Type{MapOf: &Type{TypeOf: reflect.Interface}},
	"aa": &Type{TypeOf: reflect.Int},
}}

var barResType = &Type{}

func (t TestEndpoint) Bar(r *http.Request, args *BarArgs, _ *struct{}) error {
	return nil
}

// Wat is only here to ensure we don't accidentally pick up on any methods not
// actually part of the rpc
func (t TestEndpoint) Wat(a, b int) {}

func TestGetName(t *T) {
	n, err := getName(TestEndpoint{}, "")
	assert.Equal(t, "TestEndpoint", n)
	assert.Nil(t, err)

	n, err = getName(&TestEndpoint{}, "")
	assert.Equal(t, "TestEndpoint", n)
	assert.Nil(t, err)

	n, err = getName(TestEndpoint{}, "testEndpoint")
	assert.Equal(t, "testEndpoint", n)
	assert.Nil(t, err)

}

func TestGetMethods(t *T) {
	m := getMethods(TestEndpoint{})
	require.Equal(t, 2, len(m))
	assert.True(t, m[0].Name == "Foo" || m[0].Name == "Bar")
	assert.True(t, m[1].Name == "Foo" || m[1].Name == "Bar")
}

func TestProcessType(t *T) {
	typ, err := processType(reflect.TypeOf(&FooArgs{}))
	require.Nil(t, err)
	assert.Equal(t, fooArgsType, typ)

	typ, err = processType(reflect.TypeOf(&BarArgs{}))
	require.Nil(t, err)
	assert.Equal(t, barArgsType, typ)
}

func TestGetServices(t *T) {
	s := NewServer()
	s.RegisterService(TestEndpoint{}, "")
	s.RegisterCodec(json2.NewCodec(), "application/json")

	var res GetServicesRes
	require.Nil(t, rpcutil.JSONRPC2CallHandler(s, &res, "RPC.GetServices", &struct{}{}))
	expected := []Service{{
		Name: "TestEndpoint",
		Methods: map[string]Method{
			"Foo": {
				Name:    "Foo",
				Args:    fooArgsType,
				Returns: fooResType,
			},
			"Bar": {
				Name:    "Bar",
				Args:    barArgsType,
				Returns: barResType,
			},
		},
	}}
	assert.Equal(t, expected, res.Services)

	// Quick check to make sure passthrough of actual methods works too
	args2 := FooArgs{1, "one"}
	var res2 FooRes
	require.Nil(t, rpcutil.JSONRPC2CallHandler(s, &res2, "TestEndpoint.Foo", &args2))
	assert.Equal(t, args2, res2.FooArgs)
}
