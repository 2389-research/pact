// ABOUTME: Adapts PACT bootstrap operations to stable command-line arguments and output.
// ABOUTME: Routes bootstrap and ledger commands without exposing private key bytes.
package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"pact/internal/canonical"
	"pact/internal/identity"
	"pact/internal/ledger"
	"pact/internal/store"
)

const (
	exitUsage             = 2
	exitStore             = 3
	exitIntegrity         = 4
	exitAuthorization     = 5
	exitSecretSafety      = 7
	exitMissingDependency = 9
	exitUnexpectedError   = 10
)

type commandError struct {
	code    int
	message string
	details map[string]any
}

func (err *commandError) Error() string { return err.message }

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		emitError(stderr, false, &commandError{code: exitUsage, message: "a command is required"})
		return exitUsage
	}
	command := args[0]
	commandArgs, asJSON := takeJSONFlag(args[1:])
	flagOutput := stderr
	if asJSON {
		flagOutput = io.Discard
	}
	var result map[string]any
	var err error
	switch command {
	case "init":
		result, err = runInit(commandArgs, flagOutput)
	case "keygen":
		result, err = runKeygen(commandArgs, flagOutput)
	case "trust-add":
		result, err = runTrustAdd(commandArgs, flagOutput)
	case "hash":
		result, err = runHash(commandArgs, flagOutput)
	case "commit":
		result, err = runCommit(commandArgs, flagOutput)
	case "heads":
		result, err = runHeads(commandArgs, flagOutput)
	case "show":
		result, err = runShow(commandArgs, flagOutput)
	case "verify":
		result, err = runVerify(commandArgs, flagOutput)
	case "checkpoint":
		result, err = runCheckpoint(commandArgs, flagOutput)
	case "index":
		result, err = runIndex(commandArgs, flagOutput)
	case "log":
		result, err = runLog(commandArgs, flagOutput)
	case "query":
		result, err = runQuery(commandArgs, flagOutput)
	default:
		err = &commandError{code: exitUsage, message: fmt.Sprintf("unknown command: %s", command)}
	}
	if err != nil {
		code := exitUnexpectedError
		var expected *commandError
		if errors.As(err, &expected) {
			code = expected.code
		}
		if expected != nil {
			emitError(stderr, asJSON, expected)
		} else {
			emitError(stderr, asJSON, &commandError{code: code, message: err.Error()})
		}
		return code
	}
	emitResult(stdout, asJSON, result)
	return 0
}

func runInit(args []string, stderr io.Writer) (map[string]any, error) {
	flags := flag.NewFlagSet("init", flag.ContinueOnError)
	flags.SetOutput(stderr)
	repo := flags.String("repo", ".", "project root")
	namespace := flags.String("namespace", "", "default namespace")
	if err := flags.Parse(args); err != nil {
		return nil, &commandError{code: exitUsage, message: "invalid init arguments"}
	}
	if flags.NArg() != 0 || *namespace == "" {
		return nil, &commandError{code: exitUsage, message: "init requires --namespace"}
	}
	st, err := store.Init(*repo, *namespace, time.Now())
	if err != nil {
		code := exitUnexpectedError
		switch {
		case errors.Is(err, store.ErrInvalidNamespace):
			code = exitUsage
		case errors.Is(err, store.ErrAlreadyInitialized):
			code = exitStore
		}
		return nil, &commandError{code: code, message: err.Error()}
	}
	return map[string]any{"operation": "init", "store": st.Dir(), "default_namespace": *namespace, "format": "pact/store/v1"}, nil
}

func runKeygen(args []string, stderr io.Writer) (map[string]any, error) {
	flags := flag.NewFlagSet("keygen", flag.ContinueOnError)
	flags.SetOutput(stderr)
	actor := flags.String("actor", "", "actor label")
	output := flags.String("out", "", "external key file")
	if err := flags.Parse(args); err != nil {
		return nil, &commandError{code: exitUsage, message: "invalid keygen arguments"}
	}
	if flags.NArg() != 0 || *actor == "" || *output == "" {
		return nil, &commandError{code: exitUsage, message: "keygen requires --actor and --out"}
	}
	key, err := identity.GenerateKeyFile(*output, *actor, time.Now())
	if err != nil {
		code := exitUsage
		if errors.Is(err, identity.ErrProjectKeyOutput) {
			code = exitSecretSafety
		}
		return nil, &commandError{code: code, message: err.Error()}
	}
	return map[string]any{
		"operation":  "keygen",
		"actor":      key.Actor,
		"key_id":     key.KeyID,
		"public_key": base64.RawURLEncoding.EncodeToString(key.Public),
		"path":       key.Path,
	}, nil
}

func runTrustAdd(args []string, stderr io.Writer) (map[string]any, error) {
	flags := flag.NewFlagSet("trust-add", flag.ContinueOnError)
	flags.SetOutput(stderr)
	repo := flags.String("repo", ".", "project root")
	keyPath := flags.String("key-file", "", "external key file")
	if err := flags.Parse(args); err != nil {
		return nil, &commandError{code: exitUsage, message: "invalid trust-add arguments"}
	}
	if flags.NArg() != 0 || *keyPath == "" {
		return nil, &commandError{code: exitUsage, message: "trust-add requires --key-file"}
	}
	st, err := store.Open(*repo)
	if err != nil {
		return nil, &commandError{code: exitStore, message: err.Error()}
	}
	key, err := identity.LoadPublicKey(*keyPath)
	if err != nil {
		return nil, commandErrorFor(err, exitUsage)
	}
	created, err := ledger.AddRoot(st, key, time.Now())
	if err != nil {
		return nil, commandErrorFor(err, exitUnexpectedError)
	}
	return map[string]any{
		"operation":  "trust-add",
		"key_id":     key.KeyID,
		"actor":      key.Actor,
		"created":    created,
		"trust_file": filepath.Join(st.Dir(), "trust.json"),
		"note":       "local trust bootstrap is out-of-band configuration, not ledger history",
	}, nil
}

func runHash(args []string, stderr io.Writer) (map[string]any, error) {
	flags := flag.NewFlagSet("hash", flag.ContinueOnError)
	flags.SetOutput(stderr)
	if err := flags.Parse(args); err != nil {
		return nil, &commandError{code: exitUsage, message: "invalid hash arguments"}
	}
	if flags.NArg() != 1 {
		return nil, &commandError{code: exitUsage, message: "hash requires one file"}
	}
	path, err := filepath.Abs(flags.Arg(0))
	if err != nil {
		return nil, &commandError{code: exitUsage, message: err.Error()}
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return nil, &commandError{code: exitUsage, message: fmt.Sprintf("file not found: %s", path)}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, &commandError{code: exitUsage, message: fmt.Sprintf("file not found: %s", path)}
	}
	return map[string]any{"operation": "hash", "path": path, "digest": canonical.Digest(raw), "size": len(raw)}, nil
}

func commandErrorFor(err error, defaultCode int) *commandError {
	code := defaultCode
	switch {
	case errors.Is(err, identity.ErrSecretSafety), errors.Is(err, identity.ErrProjectKeyOutput), errors.Is(err, ledger.ErrSecretSafety):
		code = exitSecretSafety
	case errors.Is(err, identity.ErrIntegrity), errors.Is(err, ledger.ErrIntegrity), errors.Is(err, store.ErrIntegrity):
		code = exitIntegrity
	case errors.Is(err, ledger.ErrCheckpointAuthorization):
		code = exitAuthorization
	case errors.Is(err, ledger.ErrStore), errors.Is(err, store.ErrNotInitialized):
		code = exitStore
	case errors.Is(err, ledger.ErrMissingDependency), errors.Is(err, store.ErrMissingDependency):
		code = exitMissingDependency
	}
	return &commandError{code: code, message: err.Error()}
}

func takeJSONFlag(args []string) ([]string, bool) {
	result := make([]string, 0, len(args))
	asJSON := false
	for _, argument := range args {
		if argument == "--json" {
			asJSON = true
			continue
		}
		result = append(result, argument)
	}
	return result, asJSON
}

func emitResult(writer io.Writer, asJSON bool, result map[string]any) {
	if asJSON {
		if err := json.NewEncoder(writer).Encode(result); err != nil {
			return
		}
		return
	}
	switch result["operation"] {
	case "index-status", "index-rebuild":
		emitIndexHuman(writer, result)
	case "log", "query":
		emitQueryHuman(writer, result)
	default:
		fmt.Fprintf(writer, "PACT %s\n", result["operation"])
	}
}

func emitError(writer io.Writer, asJSON bool, err *commandError) {
	if asJSON {
		result := map[string]any{"ok": false, "error": err.message, "exit_code": err.code}
		if err.details != nil {
			result["details"] = err.details
		}
		if encodeErr := json.NewEncoder(writer).Encode(result); encodeErr != nil {
			return
		}
		return
	}
	fprintf(writer, "PACT error: %s\n", err.message)
}

func fprintf(writer io.Writer, format string, values ...any) {
	_, _ = fmt.Fprintf(writer, format, values...)
}
