package gatewayrpc

import (
	"net/http"
	"reflect"
	"unicode"
	"unicode/utf8"

	"github.com/gorilla/rpc/v2"
)

type RPC struct {
	server *DescriptorServer
}

type GetMethodsArgs struct {
	Service string `json:"service"`
}

type GetMethodsReply struct {
	Services []*Service `json:"services"`
}

func (r *RPC) GetMethods(req *http.Request, args *GetMethodsArgs, reply *GetMethodsReply) error {
	if args.Service != "" {
		for _, v := range r.server.services {
			if v.Name == args.Service {
				reply.Services = append(reply.Services, v)
				break
			}
		}
	} else {
		reply.Services = r.server.services
	}
	return nil
}

type DescriptorServer struct {
	rpc.Server
	services []*Service
}

func NewServer(enc rpc.Server) *DescriptorServer {
	s := &DescriptorServer{enc, []*Service{}}
	err := enc.RegisterService(&RPC{s}, "")
	// this should *never* happen
	if err != nil {
		panic(err)
	}
	return s
}

var (
	// Precompute the reflect.Type of error and http.Request
	typeOfError   = reflect.TypeOf((*error)(nil)).Elem()
	typeOfRequest = reflect.TypeOf((*http.Request)(nil)).Elem()
)

//func (s *DescriptorServer) RegisterService(receiver interface{}, name string) error {
//	kv := llog.KV{"name": name}
//	llog.Debug("Registering service", kv)
//	err := s.Server.RegisterService(receiver, name)
//	if err != nil {
//		kv["error"] = err
//		llog.Error("Error registering service", kv)
//		return err
//	}
//	// after this is mostly copy/paste from map.go in github.com/gorilla/rpc/v2
//	// we're essentially duplicating what s.Server.RegisterService is doing..
//	rcvr := reflect.ValueOf(receiver)
//	rcvrType := reflect.TypeOf(receiver)
//	serv := &Service{
//		Name:     name,
//		receiver: receiver,
//		Methods:  map[string]*Method{},
//	}
//	if name == "" {
//		serv.Name = reflect.Indirect(rcvr).Type().Name()
//		if !isExported(serv.Name) {
//			llog.Error("Registered service was not exported", llog.KV{"name": serv.Name})
//			return fmt.Errorf("rpc: type %q is not exported", serv.Name)
//		}
//	}
//	if serv.Name == "" {
//		llog.Error("Registered service has no name", llog.KV{"type": rcvrType.String()})
//		return fmt.Errorf("rpc: no service name for type %q", rcvrType.String())
//	}
//	kv["name"] = serv.Name
//	llog.Info("Registering service", kv)
//	// Setup methods.
//	for i := 0; i < rcvrType.NumMethod(); i++ {
//		method := rcvrType.Method(i)
//		kv["method"] = method.Name
//		llog.Debug("Registering service method", kv)
//		mtype := method.Type
//		// Method must be exported.
//		if method.PkgPath != "" {
//			llog.Debug("Registered service method not exported", kv)
//			continue
//		}
//		// Method needs four ins: receiver, *http.Request, *args, *reply.
//		if mtype.NumIn() != 4 {
//			llog.Debug("Registered service method has not enough args", kv)
//			continue
//		}
//		// First argument must be a pointer and must be http.Request.
//		reqType := mtype.In(1)
//		if reqType.Kind() != reflect.Ptr || reqType.Elem() != typeOfRequest {
//			llog.Debug("Registered service method has invalid 1st arg", kv)
//			continue
//		}
//		// Second argument must be a pointer and must be exported.
//		args := mtype.In(2)
//		if args.Kind() != reflect.Ptr || !isExportedOrBuiltin(args) {
//			llog.Debug("Registered service method has invalid 2nd arg", kv)
//			continue
//		}
//		// Third argument must be a pointer and must be exported.
//		reply := mtype.In(3)
//		if reply.Kind() != reflect.Ptr || !isExportedOrBuiltin(reply) {
//			llog.Debug("Registered service method has invalid 3rd arg", kv)
//			continue
//		}
//		// Method needs one out: error.
//		if mtype.NumOut() != 1 {
//			llog.Debug("Registered service method has not-one return", kv)
//			continue
//		}
//		if returnType := mtype.Out(0); returnType != typeOfError {
//			llog.Debug("Registered service method has invalid return", kv)
//			continue
//		}
//		serv.Methods[method.Name] = &Method{
//			Name: method.Name,
//			//argsType:  args.Elem(),
//			//replyType: reply.Elem(),
//		}
//	}
//	if len(serv.Methods) == 0 {
//		llog.Debug("Registered service has no methods", kv)
//		return fmt.Errorf("rpc: %q has no exported methods of suitable type", serv.Name)
//	}
//	s.services = append(s.services, serv)
//	return nil
//}

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
