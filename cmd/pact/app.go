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
	"pact/internal/index"
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
	result, err := store.Init(*repo, *namespace, time.Now())
	if err != nil {
		code := exitUnexpectedError
		switch {
		case errors.Is(err, store.ErrInvalidNamespace):
			code = exitUsage
		case result.Status == store.InitConflict && store.IsCleanInitCollision(err):
			code = exitStore
		}
		return nil, &commandError{code: code, message: err.Error()}
	}
	return map[string]any{"operation": "init", "store": result.Store.Dir(), "default_namespace": *namespace, "format": "pact/store/v1"}, nil
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
	generated, err := identity.GenerateKeyFile(*output, *actor, time.Now())
	if err != nil {
		code := exitUsage
		if errors.Is(err, identity.ErrProjectKeyOutput) {
			code = exitSecretSafety
		}
		return nil, &commandError{code: code, message: err.Error()}
	}
	key := generated.Key
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
	rootResult, err := ledger.AddRoot(st, key, time.Now())
	if err != nil {
		return nil, commandErrorFor(err, exitUnexpectedError)
	}
	return map[string]any{
		"operation":  "trust-add",
		"key_id":     key.KeyID,
		"actor":      key.Actor,
		"created":    rootResult.Status == ledger.RootCreated,
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
	if typed, found := typedOwnerCommandError(err); found {
		if typed != nil {
			return typed
		}
		return &commandError{code: exitUnexpectedError, message: err.Error()}
	}
	code := defaultCode
	switch {
	case errors.Is(err, identity.ErrSecretSafety), errors.Is(err, identity.ErrProjectKeyOutput), errors.Is(err, ledger.ErrSecretSafety):
		code = exitSecretSafety
	case errors.Is(err, identity.ErrIntegrity), errors.Is(err, ledger.ErrIntegrity), errors.Is(err, store.ErrIntegrity):
		code = exitIntegrity
	case errors.Is(err, ledger.ErrCheckpointAuthorization):
		code = exitAuthorization
	case errors.Is(err, ledger.ErrStore), errors.Is(err, store.ErrNotInitialized), errors.Is(err, store.ErrAlreadyInitialized) && store.IsCleanInitCollision(err):
		code = exitStore
	case errors.Is(err, ledger.ErrMissingDependency), errors.Is(err, store.ErrMissingDependency):
		code = exitMissingDependency
	}
	return &commandError{code: code, message: err.Error()}
}

func typedOwnerCommandError(err error) (*commandError, bool) {
	tree := inspectTypedOwnerError(err)
	if !tree.found {
		return nil, false
	}
	if len(tree.limits) != 0 {
		if tree.unexpected || len(tree.limits) != 1 {
			return nil, true
		}
		for _, queryErr := range tree.queries {
			if queryErr.Code != "source_changed" {
				return nil, true
			}
		}
		limit := tree.limits[0]
		return &commandError{code: exitMissingDependency, message: limit.Error(), details: map[string]any{
			"code": "resource_limit", "resource": limit.Resource,
			"maximum": limit.Maximum, "observed_at_least": limit.ObservedAtLeast,
		}}, true
	}
	if tree.unexpected || len(tree.queries) != 1 {
		return nil, true
	}
	queryErr := tree.queries[0]
	var code int
	switch queryErr.Code {
	case "cursor_invalid", "cursor_query_mismatch":
		code = exitUsage
	case "source_invalid":
		code = exitIntegrity
	case "index_missing", "index_stale", "index_corrupt", "index_incompatible", "index_partial_build", "source_changed", "resource_limit", "cursor_stale":
		code = exitMissingDependency
	case "index_publication_failed":
		code = exitUnexpectedError
	default:
		return &commandError{code: exitUnexpectedError, message: "indexed query failed"}, true
	}
	return &commandError{code: code, message: queryErr.Error(), details: map[string]any{"code": queryErr.Code}}, true
}

type typedOwnerErrorTree struct {
	limits     []*ledger.LimitError
	queries    []*index.QueryError
	found      bool
	unexpected bool
}

func inspectTypedOwnerError(err error) typedOwnerErrorTree {
	var tree typedOwnerErrorTree
	inspectTypedOwnerErrorNode(err, &tree)
	return tree
}

func inspectTypedOwnerErrorNode(err error, tree *typedOwnerErrorTree) {
	if err == nil {
		tree.unexpected = true
		return
	}
	lockErr := &store.LockError{}
	if errors.As(err, &lockErr) && lockErr.Release != nil {
		tree.unexpected = true
	}
	if multiple, ok := err.(interface{ Unwrap() []error }); ok {
		causes := multiple.Unwrap()
		if len(causes) == 0 {
			inspectTypedOwnerErrorLeaf(err, tree)
			return
		}
		for _, cause := range causes {
			inspectTypedOwnerErrorNode(cause, tree)
		}
		return
	}
	if single, ok := err.(interface{ Unwrap() error }); ok {
		cause := single.Unwrap()
		if cause == nil {
			inspectTypedOwnerErrorLeaf(err, tree)
			return
		}
		inspectTypedOwnerErrorNode(cause, tree)
		return
	}
	inspectTypedOwnerErrorLeaf(err, tree)
}

func inspectTypedOwnerErrorLeaf(err error, tree *typedOwnerErrorTree) {
	{
		var typed *ledger.LimitError
		var typed1 *index.QueryError
		switch {
		case errors.As(err, &typed):
			tree.found = true
			if typed == nil {
				tree.unexpected = true
				return
			}
			tree.limits = append(tree.limits, typed)
		case errors.As(err, &typed1):
			tree.found = true
			if typed1 == nil {
				tree.unexpected = true
				return
			}
			tree.queries = append(tree.queries, typed1)
		default:
			tree.unexpected = true
		}
	}
}

func emitResult(writer io.Writer, asJSON bool, result map[string]any) error {
	if asJSON {
		return json.NewEncoder(writer).Encode(result)
	}
	switch result["operation"] {
	case "index-status", "index-rebuild":
		return emitIndexHuman(writer, result)
	default:
		_, err := fmt.Fprintf(writer, "PACT %s\n", result["operation"])
		return err
	}
}

func emitError(writer io.Writer, asJSON bool, err *commandError) error {
	if asJSON {
		result := map[string]any{"ok": false, "error": err.message, "exit_code": err.code}
		if err.details != nil {
			result["details"] = err.details
		}
		return json.NewEncoder(writer).Encode(result)
	}
	_, writeErr := fmt.Fprintf(writer, "PACT error: %s\n", err.message)
	return writeErr
}

func fprintf(writer io.Writer, format string, values ...any) error {
	_, err := fmt.Fprintf(writer, format, values...)
	return err
}
