# gatewayrpc

[![GoDoc](https://godoc.org/github.com/LevenLabs/gatewayrpc?status.svg)](https://godoc.org/github.com/LevenLabs/gatewayrpc)

A package which wraps a gorilla/rpc/v2 server, adding an endpoint
(`RPC.GetServices`) which automatically returns a data representation of all
methods and their signatures.

This package also contains a sub-package `gateway`, which is another library
used for creating a "gateway api", a simple rpc server which forwards requests
to other rpc servers that it discovers, based on the service portion of the rpc
call's method name. It also allows for injection of code just before the
forwarding, to allow for things like rate-limiting, authentication, and any
other behavior you might want to apply to some or all of the calls coming
through it.
