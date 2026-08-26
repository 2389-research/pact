// ABOUTME: Declares PACT's complete operator command inventory in one catalog.
// ABOUTME: Supplies dispatch, help, grouping, repository needs, and examples from one source.
package main

import (
	"io"

	"pact/internal/index"
	setuppkg "pact/internal/setup"
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
	setup  *setuppkg.Result
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
	flags      []commandFlag
}

type commandFlag struct {
	usage       string
	description string
	global      bool
}

func commandCatalog() []commandSpec {
	withPresentationFlags := func(local ...commandFlag) []commandFlag {
		return append([]commandFlag{
			{usage: "--json", description: "write machine-readable JSON", global: true},
			{usage: "--color auto|always|never", description: "control human color output", global: true},
		}, local...)
	}
	return []commandSpec{
		{[]string{"setup"}, "Get started", "Configure a usable local PACT ledger.", "pact setup [--repo PATH] --namespace NAMESPACE --actor ACTOR --key-file PATH [--json]", []string{"pact setup --namespace org/example/widget --actor Alice --key-file ../alice.key.json"}, repositoryCreate, runSetup, withPresentationFlags(commandFlag{usage: "--repo PATH", description: "project root"}, commandFlag{usage: "--namespace NAMESPACE", description: "default namespace"}, commandFlag{usage: "--actor ACTOR", description: "label the signing identity"}, commandFlag{usage: "--key-file PATH", description: "external signing key file"})},
		{[]string{"init"}, "Get started", "Initialize a PACT store.", "pact init --namespace NAMESPACE [--repo PATH]", []string{"pact init --namespace org/example/widget"}, repositoryCreate, mapHandler(runInit), withPresentationFlags(commandFlag{usage: "--namespace NAMESPACE", description: "default namespace"}, commandFlag{usage: "--repo PATH", description: "project root"})},
		{[]string{"keygen"}, "Get started", "Create an external signing key.", "pact keygen --actor ACTOR --out PATH", []string{"pact keygen --actor Alice --out alice.key.json"}, repositoryNone, mapHandler(runKeygen), withPresentationFlags(commandFlag{usage: "--actor ACTOR", description: "label the signing identity"}, commandFlag{usage: "--out PATH", description: "write the external key file"})},
		{[]string{"trust-add"}, "Get started", "Trust a public signing identity.", "pact trust-add --key-file PATH [--repo PATH]", []string{"pact trust-add --key-file alice.public.json"}, repositoryOpen, mapHandler(runTrustAdd), withPresentationFlags(commandFlag{usage: "--key-file PATH", description: "external key file"}, commandFlag{usage: "--repo PATH", description: "project root"})},
		{[]string{"status"}, "Inspect", "Check ledger and indexed-read health.", "pact status [--repo PATH] [--json]", []string{"pact status"}, repositoryOpen, runStatus, withPresentationFlags(commandFlag{usage: "--repo PATH", description: "project root"})},
		{[]string{"heads"}, "Inspect", "Show local namespace heads.", "pact heads [--repo PATH] [--namespace PREFIX] [--json]", []string{"pact heads"}, repositoryOpen, mapHandler(runHeads), withPresentationFlags(commandFlag{usage: "--repo PATH", description: "project root"}, commandFlag{usage: "--namespace PREFIX", description: "namespace prefix"})},
		{[]string{"show"}, "Inspect", "Inspect one object or event.", "pact show [--repo PATH] IDENTIFIER [--json]", []string{"pact show sha256:..."}, repositoryOpen, mapHandler(runShow), withPresentationFlags(commandFlag{usage: "--repo PATH", description: "project root"})},
		{[]string{"verify"}, "Inspect", "Verify canonical ledger state.", "pact verify [--repo PATH] [--strict] [--json]", []string{"pact verify --strict"}, repositoryOpen, mapHandler(runVerify), withPresentationFlags(commandFlag{usage: "--repo PATH", description: "project root"}, commandFlag{usage: "--strict", description: "treat missing references as errors"})},
		{[]string{"log"}, "Inspect", "Read compact causal history.", "pact log [--repo PATH] [--namespace PREFIX]... [--actor KEY_ID]... [--limit N] [--cursor TOKEN] [--json]", []string{"pact log --limit 100"}, repositoryOpen, pageHandler(runLog), withPresentationFlags(commandFlag{usage: "--repo PATH", description: "project root"}, commandFlag{usage: "--limit N", description: "maximum results"}, commandFlag{usage: "--cursor TOKEN", description: "continuation cursor"}, commandFlag{usage: "--namespace PREFIX", description: "namespace prefix"}, commandFlag{usage: "--actor KEY_ID", description: "actor key ID"})},
		{[]string{"query"}, "Inspect", "Filter causal event history.", "pact query [--repo PATH] FILTER... [--limit N] [--cursor TOKEN] [--json]", []string{"pact query --type widget.seen"}, repositoryOpen, pageHandler(runQuery), withPresentationFlags(commandFlag{usage: "--repo PATH", description: "project root"}, commandFlag{usage: "--limit N", description: "maximum results"}, commandFlag{usage: "--cursor TOKEN", description: "continuation cursor"}, commandFlag{usage: "--namespace PREFIX", description: "namespace prefix"}, commandFlag{usage: "--type TYPE", description: "event type"}, commandFlag{usage: "--kind KIND", description: "event kind"}, commandFlag{usage: "--subject SUBJECT", description: "event subject"}, commandFlag{usage: "--actor KEY_ID", description: "actor key ID"}, commandFlag{usage: "--tag TAG", description: "event tag"}, commandFlag{usage: "--schema-ref ID", description: "schema reference"}, commandFlag{usage: "--event-ref REF", description: "event reference"}, commandFlag{usage: "--caused-by REF", description: "causal event reference"}, commandFlag{usage: "--supersedes REF", description: "superseded event reference"})},
		{[]string{"commit"}, "Write", "Sign and append an event batch.", "pact commit --key-file PATH --events PATH [--repo PATH] [OPTIONS] [--json]", []string{"pact commit --key-file alice.key.json --events events.json"}, repositoryOpen, mapHandler(runCommit), withPresentationFlags(commandFlag{usage: "--repo PATH", description: "project root"}, commandFlag{usage: "--key-file PATH", description: "external key file"}, commandFlag{usage: "--events PATH", description: "event batch JSON"}, commandFlag{usage: "--namespace NAMESPACE", description: "namespace"}, commandFlag{usage: "--observed-at TIME", description: "advisory observed time"}, commandFlag{usage: "--correlation-id ID", description: "correlation ID"}, commandFlag{usage: "--parent ID", description: "parent commit ID"}, commandFlag{usage: "--delegation-ref ID", description: "unsupported in this phase"}, commandFlag{usage: "--epoch EPOCH", description: "unsupported in this phase"}, commandFlag{usage: "--lease-ref ID", description: "unsupported in this phase"})},
		{[]string{"checkpoint"}, "Write", "Sign an authorized checkpoint.", "pact checkpoint --key-file PATH --scope PREFIX --policy-ref ID --authority-epoch EPOCH [OPTIONS] [--json]", []string{"pact checkpoint --key-file alice.key.json --scope org/example --policy-ref sha256:... --authority-epoch 1"}, repositoryOpen, mapHandler(runCheckpoint), withPresentationFlags(commandFlag{usage: "--repo PATH", description: "project root"}, commandFlag{usage: "--key-file PATH", description: "external key file"}, commandFlag{usage: "--scope PREFIX", description: "namespace prefix"}, commandFlag{usage: "--policy-ref ID", description: "policy object ID"}, commandFlag{usage: "--authority-epoch EPOCH", description: "authority epoch"}, commandFlag{usage: "--schema-ref ID", description: "schema object ID"}, commandFlag{usage: "--previous ID", description: "previous checkpoint ID"}, commandFlag{usage: "--purpose TEXT", description: "checkpoint purpose"})},
		{[]string{"index", "status"}, "Maintain", "Inspect the disposable index.", "pact index status [--repo PATH] [--json]", []string{"pact index status"}, repositoryOpen, mapHandler(runIndexStatus), withPresentationFlags(commandFlag{usage: "--repo PATH", description: "project root"})},
		{[]string{"index", "rebuild"}, "Maintain", "Rebuild the disposable index.", "pact index rebuild [--repo PATH] [--json]", []string{"pact index rebuild"}, repositoryOpen, mapHandler(runIndexRebuild), withPresentationFlags(commandFlag{usage: "--repo PATH", description: "project root"})},
		{[]string{"hash"}, "Maintain", "Hash one evidence file.", "pact hash FILE [--json]", []string{"pact hash evidence.txt"}, repositoryNone, mapHandler(runHash), withPresentationFlags()},
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
