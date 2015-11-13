# gatewayrpc

A RPC server that proxies requests to other backends.

## To Test

* Start server
* Start gateway
* ``` curl -X POST -H "Content-Type: application/json" --data '{"jsonrpc": "2.0", "method": "Math.Add", "params": [1,2], "id": 1}' http://localhost:8886/rpc```
