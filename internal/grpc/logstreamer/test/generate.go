package test

//go:generate sh -c "if go run ../../../../internal/generators/can_modify_repo.go 2>/dev/null; then PATH=\"$PATH:`go env GOPATH`/bin\" protoc --go_out=. --go_opt=paths=source_relative log_test.proto; fi"
