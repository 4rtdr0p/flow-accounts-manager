package main

import _ "embed"

// openAPISpec is the embedded Wallet API OpenAPI document (single source for auth scopes).
//
//go:embed openapi.yml
var openAPISpec []byte
