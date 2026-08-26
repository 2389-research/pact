// ABOUTME: Enables the shared compiled-binary operator CLI contract for this candidate.
// ABOUTME: Keeps parser variants accountable to identical external behavior.
package e2e

import "testing"

func TestOperatorCLIContract(t *testing.T) {
	runOperatorCLIContract(t)
}
