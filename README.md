# gatewayrpc

[![GoDoc](https://godoc.org/github.com/LevenLabs/gatewayrpc?status.svg)](https://godoc.org/github.com/LevenLabs/gatewayrpc)

A package which wraps a gorilla/rpc/v2 server, adding an endpoint
(`RPC.GetServices`) which automatically returns a data representation of all
methods and their signatures.

This package also contains a sub-package `gateway`, which can be used to create
a simple rpc server which forwards requests to other rpc servers.
