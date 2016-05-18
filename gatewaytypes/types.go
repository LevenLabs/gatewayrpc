package gatewaytypes

import "reflect"

// Service describes an rpc service which has a set of methods it supports
type Service struct {
	Name    string            `json:"name"`
	Methods map[string]Method `json:"methods"`
}

// Method describes a single method of a Service. It has a name it is identified
// by and a set of arguments it accepts, as well as a single return value
type Method struct {
	Name    string `json:"name"`
	Args    *Type  `json:"args"`
	Returns *Type  `json:"returns"`
}

// Type describes a type. Only one of its fields should be a non-zero value,
// resulting in a tree structure. The leaves of the tree should all be TypeOf
// leaves.
//
// We use the pointer to Type in all cases so that one Type may optionally
// embed another
type Type struct {
	TypeOf   reflect.Kind     `json:"typeOf,omitempty"`
	ArrayOf  *Type            `json:"arrayOf,omitempty"`
	ObjectOf map[string]*Type `json:"objectOf,omitempty"`

	// This is distinct from ObjectOf in that ObjectOf has specific keys it
	// supports, and each key has a specific type. A MapOf supports any key
	// (as long as it's a string) and all values must be of the given type
	MapOf *Type `json:"mapOf,omitempty"`
}
