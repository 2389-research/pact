// ABOUTME: Declares PACT's complete operator command inventory in one catalog.
// ABOUTME: Supplies dispatch, help, grouping, repository needs, and examples from one source.
package main

import (
	"io"

	"pact/internal/index"
	statuspkg "pact/internal/status"
)

type repositoryMode string

const (
	repositoryNone   repositoryMode = "none"
	repositoryCreate repositoryMode = "create"
	repositoryOpen   repositoryMode = "open"
)

type commandResult struct {
	result map[string]any
	page   *index.QueryPage
	status *statuspkg.Result
}

type commandHandler func([]string, io.Writer, runConfig) (commandResult, error)

type commandSpec struct {
	path       []string
	group      string
	purpose    string
	usage      string
	examples   []string
	repository repositoryMode
	handler    commandHandler
}

func commandCatalog() []commandSpec {
	return []commandSpec{
		{[]string{"init"}, "Get started", "Initialize a PACT store.", "pact init --namespace NAMESPACE [--repo PATH]", []string{"pact init --namespace org/example/widget"}, repositoryCreate, mapHandler(runInit)},
		{[]string{"keygen"}, "Get started", "Create an external signing key.", "pact keygen --actor ACTOR --out PATH", []string{"pact keygen --actor Alice --out alice.key.json"}, repositoryNone, mapHandler(runKeygen)},
		{[]string{"trust-add"}, "Get started", "Trust a public signing identity.", "pact trust-add --key-file PATH [--repo PATH]", []string{"pact trust-add --key-file alice.public.json"}, repositoryOpen, mapHandler(runTrustAdd)},
		{[]string{"status"}, "Inspect", "Check ledger and indexed-read health.", "pact status [--repo PATH] [--json]", []string{"pact status"}, repositoryOpen, runStatus},
		{[]string{"heads"}, "Inspect", "Show local namespace heads.", "pact heads [--repo PATH] [--namespace PREFIX] [--json]", []string{"pact heads"}, repositoryOpen, mapHandler(runHeads)},
		{[]string{"show"}, "Inspect", "Inspect one object or event.", "pact show [--repo PATH] IDENTIFIER [--json]", []string{"pact show sha256:..."}, repositoryOpen, mapHandler(runShow)},
		{[]string{"verify"}, "Inspect", "Verify canonical ledger state.", "pact verify [--repo PATH] [--strict] [--json]", []string{"pact verify --strict"}, repositoryOpen, mapHandler(runVerify)},
		{[]string{"log"}, "Inspect", "Read compact causal history.", "pact log [--repo PATH] [--namespace PREFIX]... [--actor KEY_ID]... [--limit N] [--cursor TOKEN] [--json]", []string{"pact log --limit 100"}, repositoryOpen, pageHandler(runLog)},
		{[]string{"query"}, "Inspect", "Filter causal event history.", "pact query [--repo PATH] FILTER... [--limit N] [--cursor TOKEN] [--json]", []string{"pact query --type widget.seen"}, repositoryOpen, pageHandler(runQuery)},
		{[]string{"commit"}, "Write", "Sign and append an event batch.", "pact commit --key-file PATH --events PATH [--repo PATH] [OPTIONS] [--json]", []string{"pact commit --key-file alice.key.json --events events.json"}, repositoryOpen, mapHandler(runCommit)},
		{[]string{"checkpoint"}, "Write", "Sign an authorized checkpoint.", "pact checkpoint --key-file PATH --scope PREFIX --policy-ref ID --authority-epoch EPOCH [OPTIONS] [--json]", []string{"pact checkpoint --key-file alice.key.json --scope org/example --policy-ref sha256:... --authority-epoch 1"}, repositoryOpen, mapHandler(runCheckpoint)},
		{[]string{"index", "status"}, "Maintain", "Inspect the disposable index.", "pact index status [--repo PATH] [--json]", []string{"pact index status"}, repositoryOpen, mapHandler(runIndexStatus)},
		{[]string{"index", "rebuild"}, "Maintain", "Rebuild the disposable index.", "pact index rebuild [--repo PATH] [--json]", []string{"pact index rebuild"}, repositoryOpen, mapHandler(runIndexRebuild)},
		{[]string{"hash"}, "Maintain", "Hash one evidence file.", "pact hash FILE [--json]", []string{"pact hash evidence.txt"}, repositoryNone, mapHandler(runHash)},
	}
}

func mapHandler(handler func([]string, io.Writer) (map[string]any, error)) commandHandler {
	return func(args []string, stderr io.Writer, _ runConfig) (commandResult, error) {
		result, err := handler(args, stderr)
		return commandResult{result: result}, err
	}
}

func pageHandler(handler func([]string, io.Writer) (index.QueryPage, error)) commandHandler {
	return func(args []string, stderr io.Writer, _ runConfig) (commandResult, error) {
		page, err := handler(args, stderr)
		return commandResult{page: &page}, err
	}
}
