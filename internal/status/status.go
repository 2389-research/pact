// ABOUTME: Composes strict ledger verification and disposable-index inspection for operators.
// ABOUTME: Returns typed health without rendering, prompting, repairing, or parsing canonical files.
package status

import (
	"context"
	"fmt"

	"pact/internal/index"
	"pact/internal/ledger"
	"pact/internal/store"
)

type Health string

const (
	HealthHealthy   Health = "healthy"
	HealthAttention Health = "attention"
	HealthBroken    Health = "broken"
)

type NextAction struct {
	Reason  string `json:"reason"`
	Command string `json:"command"`
}

type Result struct {
	Health           Health
	Repo             string
	Store            string
	DefaultNamespace string
	Verification     ledger.VerifyResult
	Index            *index.Status
	NextAction       *NextAction
}

func Inspect(ctx context.Context, st *store.Store) (Result, error) {
	if ctx == nil || st == nil {
		return Result{}, fmt.Errorf("status requires a context and store")
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	// DefaultNamespace reads validated store metadata through an API without a context parameter.
	namespace, err := st.DefaultNamespace() //nolint:contextcheck
	if err != nil {
		return Result{}, err
	}
	verification, err := ledger.VerifyContext(ctx, st, true)
	if err != nil {
		return Result{}, err
	}
	result := Result{
		Health:           HealthBroken,
		Repo:             st.Root(),
		Store:            st.Dir(),
		DefaultNamespace: namespace,
		Verification:     verification,
	}
	if !verification.OK {
		return result, nil
	}
	indexStatus, err := index.New(st).Status(ctx)
	if err != nil {
		return Result{}, err
	}
	result.Index = &indexStatus
	if indexStatus.Index.State != "current" {
		result.Health = HealthAttention
		result.NextAction = &NextAction{
			Reason:  "indexed reads are not ready",
			Command: "pact index rebuild",
		}
		return result, nil
	}
	result.Health = HealthHealthy
	return result, nil
}
