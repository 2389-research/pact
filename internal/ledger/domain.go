// ABOUTME: Exposes narrow normalizers for ledger-owned names and immutable references.
// ABOUTME: Lets derived consumers reuse canonical grammar and secret checks without copying rules.
package ledger

import (
	"context"
	"fmt"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// CheckFilterValueSafety rejects secret-like raw filter text before validation or echo.
func CheckFilterValueSafety(ctx context.Context, value string) error {
	hazards, err := scanSecretHazardsContext(ctx, value, "$.filter")
	if err != nil {
		return err
	}
	if len(hazards) != 0 {
		return fmt.Errorf("%w: filter value contains secret-like material", ErrSecretSafety)
	}
	return nil
}

// NormalizeNamespace validates one ledger namespace and preserves its spelling.
func NormalizeNamespace(value string) (string, error) {
	if err := validateNamespace(value); err != nil {
		return "", fmt.Errorf("invalid namespace")
	}
	return value, nil
}

// NormalizeEventType validates one event type and preserves its spelling.
func NormalizeEventType(value string) (string, error) {
	if !eventTypePattern.MatchString(value) {
		return "", fmt.Errorf("invalid event type")
	}
	return value, nil
}

// NormalizeEventKind validates one fixed event kind.
func NormalizeEventKind(value string) (string, error) {
	if !isEventKind(value) {
		return "", fmt.Errorf("invalid event kind")
	}
	return value, nil
}

// NormalizeSubject validates and NFC-normalizes one event subject.
func NormalizeSubject(value string) (string, error) {
	if value == "" || utf8.RuneCountInString(value) > 512 {
		return "", fmt.Errorf("invalid event subject")
	}
	return norm.NFC.String(value), nil
}

// NormalizeActorKeyID validates one immutable Ed25519 actor identifier.
func NormalizeActorKeyID(value string) (string, error) {
	if !isKeyID(value) {
		return "", fmt.Errorf("invalid actor key ID")
	}
	return value, nil
}

// NormalizeTag validates and NFC-normalizes one event tag.
func NormalizeTag(value string) (string, error) {
	if value == "" || utf8.RuneCountInString(value) > 128 {
		return "", fmt.Errorf("invalid event tag")
	}
	return norm.NFC.String(value), nil
}

// NormalizeSchemaRef validates one immutable object or core schema reference.
func NormalizeSchemaRef(value string) (string, error) {
	if !digestPattern.MatchString(value) && !coreSchemaPattern.MatchString(value) {
		return "", fmt.Errorf("invalid schema reference")
	}
	return value, nil
}

// NormalizeEventRef validates one immutable event reference.
func NormalizeEventRef(value string) (string, error) {
	if !eventRefPattern.MatchString(value) {
		return "", fmt.Errorf("invalid event reference")
	}
	return value, nil
}
