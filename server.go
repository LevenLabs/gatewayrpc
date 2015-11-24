// Package gatewayrpc is a wrapper around a gorilla/rpc/v2 server which
// registers a special endpoint, "RPC.GetServices", which returns a structure
// containing a description of all services and their methods the rpc server
// supports and their type signatures
package gatewayrpc

import (
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/gorilla/rpc/v2"
)

// Server is a simple wrapper around the normal gorilla/rpc/v2 server,
// adding a couple extra features
type Server struct {
	*rpc.Server
	services []Service
}

// NewServer returns a new Server struct initialized with a gorilla/rpc/v2
// server
func NewServer() *Server {
	ns := &Server{Server: rpc.NewServer()}
	ns.Server.RegisterService(ns, "RPC")
	return ns
}

// GetServicesRes describes the structure returned from the GetServices api call
type GetServicesRes struct {
	Services []Service `json:"services"`
}

// GetServices is the actual rpc method which returns the set of services and
// their methods which are supported
func (s *Server) GetServices(r *http.Request, _ *struct{}, res *GetServicesRes) error {
	res.Services = s.services
	return nil
}

// RegisterService passes its arguments through to the underlying gorilla/rpc/v2
// server, as well as adds the given receiver's rpc methods to the Server's
// cache of method data which will be returned by the "RPC.GetMethods" endpoint.
func (s *Server) RegisterService(receiver interface{}, name string) error {
	if err := s.Server.RegisterService(receiver, name); err != nil {
		return err
	}

	name, err := getName(receiver, name)
	if err != nil {
		return err
	}

	service := Service{
		Name:    name,
		Methods: map[string]Method{},
	}
	for _, method := range getMethods(receiver) {
		methodT := method.Type
		args, err := processType(methodT.In(2))
		if err != nil {
			return err
		}
		res, err := processType(methodT.In(3))
		if err != nil {
			return err
		}
		service.Methods[method.Name] = Method{
			Name:    method.Name,
			Args:    args,
			Returns: res,
		}
	}

	s.services = append(s.services, service)

	return nil
}

var (
	typeOfError          = reflect.TypeOf((*error)(nil)).Elem()
	typeOfRequest        = reflect.TypeOf((*http.Request)(nil)).Elem()
	typeOfEmptyInterface = reflect.TypeOf((*interface{})(nil)).Elem()
)

// Since name can optionally be specified to overwrite the name of rcv
func getName(rcv interface{}, name string) (string, error) {
	if name != "" {
		return name, nil
	}
	t := reflect.TypeOf(rcv)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	rcvName := t.Name()
	if !isExported(rcvName) {
		return "", errors.New("receiver not exported")
	}
	return rcvName, nil
}

func getMethods(rcv interface{}) []reflect.Method {
	var ret []reflect.Method
	t := reflect.TypeOf(rcv)
	for i := 0; i < t.NumMethod(); i++ {
		method := t.Method(i)
		mtype := method.Type
		// Method must be exported.
		if method.PkgPath != "" {
			continue
		}
		// Method needs four ins: receiver, *http.Request, *args, *reply.
		if mtype.NumIn() != 4 {
			continue
		}
		// First argument must be a pointer and must be http.Request.
		reqType := mtype.In(1)
		if reqType.Kind() != reflect.Ptr || reqType.Elem() != typeOfRequest {
			continue
		}
		// Second argument must be a pointer and must be exported.
		args := mtype.In(2)
		if args.Kind() != reflect.Ptr || !isExportedOrBuiltin(args) {
			continue
		}
		// Third argument must be a pointer and must be exported.
		reply := mtype.In(3)
		if reply.Kind() != reflect.Ptr || !isExportedOrBuiltin(reply) {
			continue
		}
		// Method needs one out: error.
		if mtype.NumOut() != 1 {
			continue
		}
		if returnType := mtype.Out(0); returnType != typeOfError {
			continue
		}
		ret = append(ret, method)
	}
	return ret
}

func processType(t reflect.Type) (*Type, error) {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	kind := t.Kind()

	// Bool through floats encompasses all integer and float types. Plus string
	if (kind >= reflect.Bool && kind <= reflect.Float64) || kind == reflect.String {
		return &Type{TypeOf: kind}, nil
	}

	if kind == reflect.Array || kind == reflect.Slice {
		innerT, err := processType(t.Elem())
		if err != nil {
			return nil, err
		}
		return &Type{ArrayOf: innerT}, nil
	}

	if kind == reflect.Map {
		if t.Key().Kind() != reflect.String {
			return nil, fmt.Errorf("unsupported map type: %v", t)
		}

		innerT, err := processType(t.Elem())
		if err != nil {
			return nil, err
		}
		return &Type{MapOf: innerT}, nil
	}

	if kind == reflect.Interface {
		if t != typeOfEmptyInterface {
			return nil, fmt.Errorf("unsupported interface: %v", t)
		}
		return &Type{TypeOf: kind}, nil
	}

	if kind == reflect.Struct {
		m := map[string]*Type{}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if !isExported(f.Name) {
				continue
			}
			key := getFieldKey(f)
			innerT, err := processType(f.Type)
			if err != nil {
				return nil, err
			}

			if f.Anonymous && len(innerT.ObjectOf) > 0 {
				for k, v := range innerT.ObjectOf {
					m[k] = v
				}
			} else {
				m[key] = innerT
			}
		}
		return &Type{ObjectOf: m}, nil
	}

	return nil, fmt.Errorf("unsupported type: %v", t)
}

func getFieldKey(f reflect.StructField) string {
	key := f.Name
	jsonTag := f.Tag.Get("json")
	if jsonTag == "" {
		return key
	}

	parts := strings.SplitN(jsonTag, ",", 2)
	if len(parts) == 0 {
		return key
	} else if parts[0] == "" {
		return key
	}

	return parts[0]
}

// isExported returns true of a string is an exported (upper case) name.
func isExported(name string) bool {
	r, _ := utf8.DecodeRuneInString(name)
	return unicode.IsUpper(r)
}

// isExportedOrBuiltin returns true if a type is exported or a builtin.
func isExportedOrBuiltin(t reflect.Type) bool {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	// PkgPath will be non-empty even for an exported type,
	// so we need to check the type name as well.
	return isExported(t.Name()) || t.PkgPath() == ""
}
