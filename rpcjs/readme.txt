用json/msgp编解码传输的标准RPC（net/rpc）覆盖实现。

msgp Options
-o      - output file name (default is {input}_gen.go)
-file   - input file name (default is $GOFILE, which are set by the go generate command)
-io     - satisfy the msgp.Decodable and msgp.Encodable interfaces (default is true)
-marshal - satisfy the msgp.Marshaler and msgp.Unmarshaler interfaces (default is true)
-tests  - generate tests and benchmarks (default is true)

