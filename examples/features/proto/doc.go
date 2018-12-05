package proto

//go:generate protoc -I ./echo --go_out=plugins=grpc:./echo ./echo/echo.proto
