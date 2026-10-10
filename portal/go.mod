// Portal contains no Go code. This module exists as a boundary: the Go tool
// does not skip node_modules, so without it every `go build ./...`,
// `go vet ./...`, `go test ./...`, and `go mod tidy` at the repository root
// walks into portal/node_modules and compiles whatever Go sources npm
// packages happen to ship (the `flatted` package ships one today). Declaring
// a nested module keeps those paths out of the root module's `./...`, the
// same way each eval/ fixture does.
//
// Any directory that runs `npm install` needs one of these.
module portal

go 1.26.9
