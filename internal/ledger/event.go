// ABOUTME: Validates and normalizes PACT event batches before immutable signing.
// ABOUTME: Rejects malformed references and likely secret material with safe diagnostics.
package ledger

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"pact/internal/canonical"

	"golang.org/x/text/unicode/norm"
)

var (
	localIDPattern     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	eventTypePattern   = regexp.MustCompile(`^[a-z0-9][a-z0-9._/-]{0,255}$`)
	digestPattern      = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	coreSchemaPattern  = regexp.MustCompile(`^pact:core/[a-z0-9._/-]+/v[0-9]+$`)
	localRefPattern    = regexp.MustCompile(`^local:([A-Za-z0-9][A-Za-z0-9._-]{0,127})$`)
	eventRefPattern    = regexp.MustCompile(`^pact:event:(sha256:[0-9a-f]{64})#([A-Za-z0-9][A-Za-z0-9._-]{0,127})$`)
	privateKeyPattern  = regexp.MustCompile(`-----BEGIN (?:RSA |EC |OPENSSH |)?PRIVATE KEY-----`)
	bearerPattern      = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/=-]{16,}`)
	jwtPattern         = regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{12,}\.[A-Za-z0-9_-]{12,}\.[A-Za-z0-9_-]{12,}\b`)
	githubTokenPattern = regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{20,}\b`)
	awsKeyPattern      = regexp.MustCompile(`\b(?:AKIA|ASIA)[A-Z0-9]{16}\b`)
	envNamePattern     = regexp.MustCompile(`^[A-Z][A-Z0-9_]{2,127}$`)
)

var secretFieldNames = map[string]struct{}{
	"password": {}, "passwd": {}, "secret": {}, "client_secret": {}, "api_key": {}, "apikey": {}, "access_token": {},
	"refresh_token": {}, "bearer_token": {}, "private_key": {}, "authorization": {}, "cookie": {}, "session_cookie": {},
}

const maximumDiagnosticSamples = 100

// EventBatch is the normalized immutable event input used in a signed commit.
type EventBatch struct {
	Namespace     string
	ObservedAt    string
	CorrelationID string
	Metadata      map[string]any
	Events        []Event
}

// Event is a normalized PACT semantic event.
type Event struct {
	LocalID, Kind, Type, Subject, SchemaRef string
	Payload                                 map[string]any
	Evidence                                []map[string]any
	CausedBy, Supersedes, Tags              []string
}

// NormalizeEventBatch validates exact batch fields and returns the stored form.
func NormalizeEventBatch(value map[string]any) (EventBatch, error) {
	return normalizeEventBatchContext(context.Background(), value)
}

func normalizeEventBatchContext(ctx context.Context, value map[string]any) (EventBatch, error) {
	if err := ctx.Err(); err != nil {
		return EventBatch{}, err
	}
	if err := exactKeys(value, []string{"events"}, []string{"namespace", "observed_at", "correlation_id", "metadata"}, "$"); err != nil {
		return EventBatch{}, err
	}
	eventsRaw, ok := value["events"].([]any)
	if !ok || len(eventsRaw) == 0 {
		return EventBatch{}, fmt.Errorf("event batch must contain at least one event")
	}
	if uint64(len(eventsRaw)) > Phase2Limits.EventsPerCommit {
		return EventBatch{}, limitError("events_per_commit", Phase2Limits.EventsPerCommit)
	}
	hazards, err := scanSecretHazardsContext(ctx, value, "$")
	if err != nil {
		return EventBatch{}, err
	}
	if len(hazards) != 0 {
		message, _ := boundedDiagnosticJoin("refusing to sign immutable secret-like material: ", hazards, "; ", Phase2Limits.DiagnosticTextBytes)
		return EventBatch{}, fmt.Errorf("%w: %s", ErrSecretSafety, message)
	}
	events, localIDs, err := normalizeBatchEventsContext(ctx, value["events"])
	if err != nil {
		return EventBatch{}, err
	}
	sort.Slice(events, func(i, j int) bool { return events[i].LocalID < events[j].LocalID })
	if err := validateLocalCausesContext(ctx, events, localIDs); err != nil {
		return EventBatch{}, err
	}
	result := EventBatch{Events: events, Metadata: map[string]any{}}
	if err := normalizeBatchFields(value, &result); err != nil {
		return EventBatch{}, err
	}
	return result, nil
}

func normalizeBatchEventsContext(ctx context.Context, value any) ([]Event, map[string]struct{}, error) {
	eventsRaw, ok := value.([]any)
	if !ok || len(eventsRaw) == 0 {
		return nil, nil, fmt.Errorf("event batch must contain at least one event")
	}
	localIDs := map[string]struct{}{}
	events := make([]Event, 0, len(eventsRaw))
	for index, raw := range eventsRaw {
		if err := pollContext(ctx, index); err != nil {
			return nil, nil, err
		}
		eventObject, ok := raw.(map[string]any)
		if !ok {
			return nil, nil, fmt.Errorf("$.events[%d]: event must be an object", index)
		}
		event, err := normalizeEventContext(ctx, eventObject, fmt.Sprintf("$.events[%d]", index), localIDs)
		if err != nil {
			return nil, nil, err
		}
		events = append(events, event)
	}
	return events, localIDs, nil
}

func validateLocalCausesContext(ctx context.Context, events []Event, localIDs map[string]struct{}) error {
	work := 0
	for _, event := range events {
		for _, ref := range event.CausedBy {
			if err := pollContext(ctx, work); err != nil {
				return err
			}
			work++
			match := localRefPattern.FindStringSubmatch(ref)
			if match == nil {
				continue
			}
			if _, found := localIDs[match[1]]; !found {
				return fmt.Errorf("event %q has a missing same-commit caused_by reference", event.LocalID)
			}
			if match[1] == event.LocalID {
				return fmt.Errorf("event %q has a self-referencing caused_by entry", event.LocalID)
			}
		}
	}
	return nil
}

func normalizeBatchFields(value map[string]any, result *EventBatch) error {
	if namespace, exists := value["namespace"]; exists {
		text, ok := namespace.(string)
		if !ok {
			return fmt.Errorf("invalid namespace")
		}
		if err := validateNamespace(text); err != nil {
			return err
		}
		result.Namespace = text
	}
	if observed, exists := value["observed_at"]; exists {
		text, ok := observed.(string)
		if !ok || text == "" || utf8.RuneCountInString(text) > 64 {
			return fmt.Errorf("observed_at must be a short timestamp string")
		}
		result.ObservedAt = norm.NFC.String(text)
	}
	if correlation, exists := value["correlation_id"]; exists {
		text, ok := correlation.(string)
		if !ok || utf8.RuneCountInString(text) > 255 {
			return fmt.Errorf("correlation_id must be a string no longer than 255 characters")
		}
		result.CorrelationID = norm.NFC.String(text)
	}
	if metadata, exists := value["metadata"]; exists {
		object, ok := metadata.(map[string]any)
		if !ok {
			return fmt.Errorf("metadata must be an object")
		}
		normalized, err := normalizeObject(object)
		if err != nil {
			return fmt.Errorf("$.metadata: %w", err)
		}
		result.Metadata = normalized
	}
	return nil
}

func normalizeEventContext(ctx context.Context, value map[string]any, path string, localIDs map[string]struct{}) (Event, error) {
	if err := ctx.Err(); err != nil {
		return Event{}, err
	}
	required := []string{"local_id", "kind", "type", "subject", "schema_ref", "payload", "evidence", "caused_by", "supersedes", "tags"}
	if err := exactKeys(value, required, nil, path); err != nil {
		return Event{}, err
	}
	result, err := normalizeEventIdentity(value, path, localIDs)
	if err != nil {
		return Event{}, err
	}
	payload, ok := value["payload"].(map[string]any)
	if !ok {
		return Event{}, fmt.Errorf("%s.payload: payload must be an object", path)
	}
	result.Payload, err = normalizeObject(payload)
	if err != nil {
		return Event{}, fmt.Errorf("%s.payload: %w", path, err)
	}
	result.Evidence, err = normalizeEvidenceContext(ctx, value["evidence"], path)
	if err != nil {
		return Event{}, err
	}
	result.CausedBy, err = normalizeRefsContext(ctx, value["caused_by"], true, path+".caused_by")
	if err != nil {
		return Event{}, err
	}
	result.Supersedes, err = normalizeRefsContext(ctx, value["supersedes"], false, path+".supersedes")
	if err != nil {
		return Event{}, err
	}
	result.Tags, err = normalizeTagsContext(ctx, value["tags"], path+".tags")
	return result, err
}

func normalizeEventIdentity(value map[string]any, path string, localIDs map[string]struct{}) (Event, error) {
	localID, ok := value["local_id"].(string)
	if !ok || !localIDPattern.MatchString(localID) {
		return Event{}, fmt.Errorf("%s.local_id: invalid local event ID", path)
	}
	if _, found := localIDs[localID]; found {
		return Event{}, fmt.Errorf("%s.local_id: duplicate local event ID %q", path, localID)
	}
	localIDs[localID] = struct{}{}
	kind, ok := value["kind"].(string)
	if !ok || !isEventKind(kind) {
		return Event{}, fmt.Errorf("%s.kind: invalid event kind", path)
	}
	eventType, ok := value["type"].(string)
	if !ok || !eventTypePattern.MatchString(eventType) {
		return Event{}, fmt.Errorf("%s.type: invalid event type", path)
	}
	subject, ok := value["subject"].(string)
	if !ok || subject == "" || utf8.RuneCountInString(subject) > 512 {
		return Event{}, fmt.Errorf("%s.subject: invalid subject", path)
	}
	schemaRef, ok := value["schema_ref"].(string)
	if !ok || (!digestPattern.MatchString(schemaRef) && !coreSchemaPattern.MatchString(schemaRef)) {
		return Event{}, fmt.Errorf("%s.schema_ref: invalid schema reference", path)
	}
	return Event{LocalID: localID, Kind: kind, Type: eventType, Subject: norm.NFC.String(subject), SchemaRef: schemaRef}, nil
}

func exactKeys(value map[string]any, required, optional []string, path string) error {
	allowed := map[string]bool{}
	missing := make([]string, 0)
	for _, key := range required {
		allowed[key] = true
		if _, found := value[key]; !found {
			missing = append(missing, key)
		}
	}
	for _, key := range optional {
		allowed[key] = true
	}
	sort.Strings(missing)
	if len(missing) != 0 {
		return fmt.Errorf("%s: missing required fields: %s", path, strings.Join(missing, ", "))
	}
	extra := make([]string, 0, maximumDiagnosticSamples)
	for key := range value {
		if !allowed[key] {
			extra = insertBoundedSortedString(extra, key, maximumDiagnosticSamples)
		}
	}
	if len(extra) != 0 {
		message, truncated := boundedDiagnosticJoin(path+": unsupported fields: ", extra, ", ", Phase2Limits.DiagnosticTextBytes)
		return &validationDiagnosticError{message: message, truncated: truncated}
	}
	return nil
}

func insertBoundedSortedString(values []string, value string, maximum int) []string {
	index := sort.SearchStrings(values, value)
	if index >= maximum || index < len(values) && values[index] == value {
		return values
	}
	values = append(values, "")
	copy(values[index+1:], values[index:])
	values[index] = value
	if len(values) > maximum {
		values = values[:maximum]
	}
	return values
}

func boundedDiagnosticJoin(prefix string, values []string, separator string, maximum uint64) (string, bool) {
	builder := newBoundedDiagnosticBuilder(maximum)
	builder.write(prefix)
	for index, value := range values {
		if index != 0 {
			builder.write(separator)
		}
		builder.write(value)
	}
	return builder.String(), builder.truncated
}

func quotedValidationError(prefix, value string) error {
	builder := newBoundedDiagnosticBuilder(Phase2Limits.DiagnosticTextBytes)
	builder.write(prefix)
	builder.write(`"`)
	builder.write(value)
	builder.write(`"`)
	return &validationDiagnosticError{message: builder.String(), truncated: builder.truncated}
}

type validationDiagnosticError struct {
	message   string
	truncated bool
}

func (err *validationDiagnosticError) Error() string { return err.message }
func validateNamespace(value string) error {
	if value == "" || len(value) > 512 || strings.HasPrefix(value, "/") || strings.HasSuffix(value, "/") || strings.Contains(value, "//") {
		return quotedValidationError("invalid namespace: ", value)
	}
	for index, character := range value {
		if (index == 0 && !asciiAlphaNumeric(character)) || (!asciiAlphaNumeric(character) && character != '.' && character != '_' && character != '/' && character != '-') {
			return quotedValidationError("invalid namespace: ", value)
		}
	}
	for segment := range strings.SplitSeq(value, "/") {
		if segment == "." || segment == ".." {
			return quotedValidationError("invalid namespace: ", value)
		}
	}
	return nil
}
func asciiAlphaNumeric(value rune) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9'
}
func isEventKind(value string) bool {
	return value == "observation" || value == "assertion" || value == "action" || value == "decision" || value == "control"
}
func normalizeRefsContext(ctx context.Context, value any, allowLocal bool, path string) ([]string, error) {
	raw, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("%s: must be an array", path)
	}
	values := map[string]struct{}{}
	for index, item := range raw {
		if err := pollContext(ctx, index); err != nil {
			return nil, err
		}
		ref, ok := item.(string)
		if !ok || (!eventRefPattern.MatchString(ref) && (!allowLocal || !localRefPattern.MatchString(ref))) {
			return nil, fmt.Errorf("%s: invalid event reference", path)
		}
		values[ref] = struct{}{}
	}
	return sortedKeysContext(ctx, values)
}
func normalizeTagsContext(ctx context.Context, value any, path string) ([]string, error) {
	raw, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("%s: must be an array", path)
	}
	values := map[string]struct{}{}
	for index, item := range raw {
		if err := pollContext(ctx, index); err != nil {
			return nil, err
		}
		tag, ok := item.(string)
		if !ok || tag == "" || utf8.RuneCountInString(tag) > 128 {
			return nil, fmt.Errorf("%s: invalid tag", path)
		}
		values[norm.NFC.String(tag)] = struct{}{}
	}
	return sortedKeysContext(ctx, values)
}
func normalizeEvidenceContext(ctx context.Context, value any, path string) ([]map[string]any, error) {
	raw, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("%s.evidence: must be an array", path)
	}
	result := make([]map[string]any, 0, len(raw))
	for index, item := range raw {
		if err := pollContext(ctx, index); err != nil {
			return nil, err
		}
		normalized, err := normalizeEvidenceEntry(item, fmt.Sprintf("%s.evidence[%d]", path, index))
		if err != nil {
			return nil, err
		}
		result = append(result, normalized)
	}
	return result, nil
}

func normalizeEvidenceEntry(value any, path string) (map[string]any, error) {
	object, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: evidence entry must be an object", path)
	}
	if err := exactKeys(object, []string{"ref", "digest", "media_type", "role"}, []string{"redacted", "description"}, path); err != nil {
		return nil, err
	}
	ref, refOK := object["ref"].(string)
	digest, digestOK := object["digest"].(string)
	media, mediaOK := object["media_type"].(string)
	role, roleOK := object["role"].(string)
	if !refOK || ref == "" || utf8.RuneCountInString(ref) > 2048 || !digestOK || !digestPattern.MatchString(digest) || !mediaOK || media == "" || utf8.RuneCountInString(media) > 255 || !roleOK || (role != "primary" && role != "supporting" && role != "derived") {
		return nil, fmt.Errorf("%s: invalid evidence entry", path)
	}
	normalized := map[string]any{"ref": norm.NFC.String(ref), "digest": digest, "media_type": norm.NFC.String(media), "role": role}
	if redacted, found := object["redacted"]; found {
		boolean, ok := redacted.(bool)
		if !ok {
			return nil, fmt.Errorf("%s.redacted: must be boolean", path)
		}
		normalized["redacted"] = boolean
	}
	if description, found := object["description"]; found {
		text, ok := description.(string)
		if !ok || utf8.RuneCountInString(text) > 512 {
			return nil, fmt.Errorf("%s.description: invalid description", path)
		}
		normalized["description"] = norm.NFC.String(text)
	}
	return normalized, nil
}
func normalizeObject(value map[string]any) (map[string]any, error) {
	normalized, err := canonical.Marshal(value)
	if err != nil {
		return nil, err
	}
	parsed, err := canonical.Parse(normalized)
	if err != nil {
		return nil, err
	}
	object, ok := parsed.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("object must be an object")
	}
	return object, nil
}
func batchValue(batch EventBatch) map[string]any {
	events := make([]any, 0, len(batch.Events))
	for _, event := range batch.Events {
		events = append(events, eventValue(event))
	}
	result := map[string]any{"events": events, "metadata": batch.Metadata}
	if batch.Namespace != "" {
		result["namespace"] = batch.Namespace
	}
	if batch.ObservedAt != "" {
		result["observed_at"] = batch.ObservedAt
	}
	if batch.CorrelationID != "" {
		result["correlation_id"] = batch.CorrelationID
	}
	return result
}
func eventValue(event Event) map[string]any {
	return map[string]any{"local_id": event.LocalID, "kind": event.Kind, "type": event.Type, "subject": event.Subject, "schema_ref": event.SchemaRef, "payload": event.Payload, "evidence": mapsToAny(event.Evidence), "caused_by": stringsToAny(event.CausedBy), "supersedes": stringsToAny(event.Supersedes), "tags": stringsToAny(event.Tags)}
}
func mapsToAny(value []map[string]any) []any {
	result := make([]any, len(value))
	for i := range value {
		result[i] = value[i]
	}
	return result
}
func stringsToAny(value []string) []any {
	result := make([]any, len(value))
	for i := range value {
		result[i] = value[i]
	}
	return result
}
func scanSecretHazards(value any, path string) []string {
	hazards, _ := scanSecretHazardsContext(context.Background(), value, path)
	return hazards
}

func scanSecretHazardsContext(ctx context.Context, value any, path string) ([]string, error) {
	hazards := map[string]struct{}{}
	work := 0
	if err := collectSecretHazardsContext(ctx, value, path, hazards, &work); err != nil {
		return nil, err
	}
	return sortedKeysContext(ctx, hazards)
}

func collectSecretHazardsContext(ctx context.Context, current any, path string, hazards map[string]struct{}, work *int) error {
	if err := pollContext(ctx, *work); err != nil {
		return err
	}
	(*work)++
	if uint64(len(hazards)) >= Phase2Limits.DiagnosticSamples {
		return nil
	}
	switch item := current.(type) {
	case map[string]any:
		for key, child := range item {
			if err := pollContext(ctx, *work); err != nil {
				return err
			}
			(*work)++
			childPath := path + "." + key
			if text, ok := child.(string); ok && isSecretField(key) && !looksRedactedOrIndirect(text) {
				hazards[childPath+": secret-like field value"] = struct{}{}
			}
			if err := collectSecretHazardsContext(ctx, child, childPath, hazards, work); err != nil {
				return err
			}
		}
	case []any:
		for index, child := range item {
			if err := collectSecretHazardsContext(ctx, child, fmt.Sprintf("%s[%d]", path, index), hazards, work); err != nil {
				return err
			}
		}
	case string:
		return collectStringHazardsContext(ctx, item, path, hazards, work)
	}
	return nil
}

func collectStringHazardsContext(ctx context.Context, value, path string, hazards map[string]struct{}, work *int) error {
	for _, candidate := range []struct {
		label   string
		pattern *regexp.Regexp
	}{{"private key material", privateKeyPattern}, {"bearer credential", bearerPattern}, {"JWT-like credential", jwtPattern}, {"GitHub token-like credential", githubTokenPattern}, {"AWS access-key-like credential", awsKeyPattern}} {
		if err := pollContext(ctx, *work); err != nil {
			return err
		}
		(*work)++
		if candidate.pattern.MatchString(value) {
			hazards[path+": "+candidate.label] = struct{}{}
		}
	}
	parsed, _ := url.Parse(value) // Malformed text is not treated as a credential-bearing URL.
	if parsed == nil || parsed.Scheme == "" {
		return nil
	}
	if parsed.User != nil {
		hazards[path+": credential-bearing URL userinfo"] = struct{}{}
	}
	for key, values := range parsed.Query() {
		if err := pollContext(ctx, *work); err != nil {
			return err
		}
		(*work)++
		if !isSecretField(key) {
			continue
		}
		for _, queryValue := range values {
			if err := pollContext(ctx, *work); err != nil {
				return err
			}
			(*work)++
			if !looksRedactedOrIndirect(queryValue) {
				hazards[path+": secret-like URL query parameter "+fmt.Sprintf("%q", key)] = struct{}{}
				break
			}
		}
	}
	return nil
}

func isSecretField(key string) bool {
	_, found := secretFieldNames[strings.ToLower(key)]
	return found
}
func looksRedactedOrIndirect(value string) bool {
	trimmed := strings.ToLower(strings.TrimSpace(value))
	if trimmed == "" || trimmed == "redacted" || trimmed == "[redacted]" || trimmed == "<redacted>" || trimmed == "***" || trimmed == "none" || trimmed == "null" {
		return true
	}
	original := strings.TrimSpace(value)
	if envNamePattern.MatchString(original) || strings.HasPrefix(original, "$") && envNamePattern.MatchString(strings.TrimPrefix(original, "$")) || strings.HasPrefix(original, "${") && strings.HasSuffix(original, "}") && envNamePattern.MatchString(original[2:len(original)-1]) {
		return true
	}
	return false
}
