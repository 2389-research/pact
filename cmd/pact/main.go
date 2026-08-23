// ABOUTME: Starts the PACT command-line program and delegates argument handling to the app.
// ABOUTME: Keeps process exit behavior out of the testable command adapter.
package main

import (
	"os"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
