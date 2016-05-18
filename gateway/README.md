# gateway

[![GoDoc](https://godoc.org/github.com/LevenLabs/gatewayrpc/gateway?status.svg)](https://godoc.org/github.com/LevenLabs/gatewayrpc/gateway)

Gateway is library used for creating a "gateway api", a simple rpc server which
forwards requests to other rpc servers that it discovers, based on the service
portion of the rpc call's method name.

It also allows for injection of code just before the forwarding, to allow for things
like rate-limiting, authentication, and any other behavior you might want to apply
to some or all of the calls coming through it.

You can also define methods and services internal to gateway to handle things like
sessions or helper methods.
