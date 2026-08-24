// ABOUTME: Validates and normalizes PACT event batches before immutable signing.
// ABOUTME: Rejects malformed references and likely secret material with safe diagnostics.
package ledger

import (
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
	if err := exactKeys(value, []string{"events"}, []string{"namespace", "observed_at", "correlation_id", "metadata"}, "$"); err != nil {
		return EventBatch{}, err
	}
	eventsRaw, ok := value["events"].([]any)
	if !ok || len(eventsRaw) == 0 {
		return EventBatch{}, fmt.Errorf("event batch must contain at least one event")
	}
	localIDs := map[string]struct{}{}
	events := make([]Event, 0, len(eventsRaw))
	for index, raw := range eventsRaw {
		eventObject, ok := raw.(map[string]any)
		if !ok {
			return EventBatch{}, fmt.Errorf("$.events[%d]: event must be an object", index)
		}
		event, err := normalizeEvent(eventObject, fmt.Sprintf("$.events[%d]", index), localIDs)
		if err != nil {
			return EventBatch{}, err
		}
		events = append(events, event)
	}
	sort.Slice(events, func(i, j int) bool { return events[i].LocalID < events[j].LocalID })
	for _, event := range events {
		for _, ref := range event.CausedBy {
			if match := localRefPattern.FindStringSubmatch(ref); match != nil {
				if _, found := localIDs[match[1]]; !found {
					return EventBatch{}, fmt.Errorf("event %q references missing same-commit event %q", event.LocalID, ref)
				}
				if match[1] == event.LocalID {
					return EventBatch{}, fmt.Errorf("event %q cannot cause itself", event.LocalID)
				}
			}
		}
	}
	result := EventBatch{Events: events, Metadata: map[string]any{}}
	if namespace, exists := value["namespace"]; exists {
		text, ok := namespace.(string)
		if !ok {
			return EventBatch{}, fmt.Errorf("invalid namespace")
		}
		if err := validateNamespace(text); err != nil {
			return EventBatch{}, err
		}
		result.Namespace = text
	}
	if observed, exists := value["observed_at"]; exists {
		text, ok := observed.(string)
		if !ok || text == "" || utf8.RuneCountInString(text) > 64 {
			return EventBatch{}, fmt.Errorf("observed_at must be a short timestamp string")
		}
		result.ObservedAt = norm.NFC.String(text)
	}
	if correlation, exists := value["correlation_id"]; exists {
		text, ok := correlation.(string)
		if !ok || utf8.RuneCountInString(text) > 255 {
			return EventBatch{}, fmt.Errorf("correlation_id must be a string no longer than 255 characters")
		}
		result.CorrelationID = norm.NFC.String(text)
	}
	if metadata, exists := value["metadata"]; exists {
		object, ok := metadata.(map[string]any)
		if !ok {
			return EventBatch{}, fmt.Errorf("metadata must be an object")
		}
		normalized, err := normalizeObject(object)
		if err != nil {
			return EventBatch{}, fmt.Errorf("$.metadata: %w", err)
		}
		result.Metadata = normalized
	}
	if hazards := scanSecretHazards(batchValue(result), "$"); len(hazards) != 0 {
		return EventBatch{}, fmt.Errorf("refusing to sign immutable secret-like material: %s", strings.Join(hazards, "; "))
	}
	return result, nil
}

func normalizeEvent(value map[string]any, path string, localIDs map[string]struct{}) (Event, error) {
	required := []string{"local_id", "kind", "type", "subject", "schema_ref", "payload", "evidence", "caused_by", "supersedes", "tags"}
	if err := exactKeys(value, required, nil, path); err != nil {
		return Event{}, err
	}
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
	if !ok || !(digestPattern.MatchString(schemaRef) || coreSchemaPattern.MatchString(schemaRef)) {
		return Event{}, fmt.Errorf("%s.schema_ref: invalid schema reference", path)
	}
	payload, ok := value["payload"].(map[string]any)
	if !ok {
		return Event{}, fmt.Errorf("%s.payload: payload must be an object", path)
	}
	normalizedPayload, err := normalizeObject(payload)
	if err != nil {
		return Event{}, fmt.Errorf("%s.payload: %w", path, err)
	}
	evidence, err := normalizeEvidence(value["evidence"], path)
	if err != nil {
		return Event{}, err
	}
	causedBy, err := normalizeRefs(value["caused_by"], true, path+".caused_by")
	if err != nil {
		return Event{}, err
	}
	supersedes, err := normalizeRefs(value["supersedes"], false, path+".supersedes")
	if err != nil {
		return Event{}, err
	}
	tags, err := normalizeTags(value["tags"], path+".tags")
	if err != nil {
		return Event{}, err
	}
	return Event{LocalID: localID, Kind: kind, Type: eventType, Subject: norm.NFC.String(subject), SchemaRef: schemaRef, Payload: normalizedPayload, Evidence: evidence, CausedBy: causedBy, Supersedes: supersedes, Tags: tags}, nil
}

func exactKeys(value map[string]any, required, optional []string, path string) error {
	allowed := map[string]bool{}
	for _, key := range required {
		allowed[key] = true
		if _, found := value[key]; !found {
			return fmt.Errorf("%s: missing required fields: %s", path, key)
		}
	}
	for _, key := range optional {
		allowed[key] = true
	}
	for key := range value {
		if !allowed[key] {
			return fmt.Errorf("%s: unsupported fields: %s", path, key)
		}
	}
	return nil
}
func validateNamespace(value string) error {
	if value == "" || len(value) > 512 || strings.HasPrefix(value, "/") || strings.HasSuffix(value, "/") || strings.Contains(value, "//") {
		return fmt.Errorf("invalid namespace: %q", value)
	}
	for index, character := range value {
		if (index == 0 && !asciiAlphaNumeric(character)) || (!asciiAlphaNumeric(character) && character != '.' && character != '_' && character != '/' && character != '-') {
			return fmt.Errorf("invalid namespace: %q", value)
		}
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == "." || segment == ".." {
			return fmt.Errorf("invalid namespace: %q", value)
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
func normalizeRefs(value any, allowLocal bool, path string) ([]string, error) {
	raw, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("%s: must be an array", path)
	}
	values := map[string]struct{}{}
	for _, item := range raw {
		ref, ok := item.(string)
		if !ok || !(eventRefPattern.MatchString(ref) || allowLocal && localRefPattern.MatchString(ref)) {
			return nil, fmt.Errorf("invalid %s: %q", path, item)
		}
		values[ref] = struct{}{}
	}
	result := make([]string, 0, len(values))
	for item := range values {
		result = append(result, item)
	}
	sort.Strings(result)
	return result, nil
}
func normalizeTags(value any, path string) ([]string, error) {
	raw, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("%s: must be an array", path)
	}
	values := map[string]struct{}{}
	for _, item := range raw {
		tag, ok := item.(string)
		if !ok || tag == "" || utf8.RuneCountInString(tag) > 128 {
			return nil, fmt.Errorf("%s: invalid tag", path)
		}
		values[norm.NFC.String(tag)] = struct{}{}
	}
	result := make([]string, 0, len(values))
	for item := range values {
		result = append(result, item)
	}
	sort.Strings(result)
	return result, nil
}
func normalizeEvidence(value any, path string) ([]map[string]any, error) {
	raw, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("%s.evidence: must be an array", path)
	}
	result := make([]map[string]any, 0, len(raw))
	for index, item := range raw {
		object, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s.evidence[%d]: evidence entry must be an object", path, index)
		}
		if err := exactKeys(object, []string{"ref", "digest", "media_type", "role"}, []string{"redacted", "description"}, fmt.Sprintf("%s.evidence[%d]", path, index)); err != nil {
			return nil, err
		}
		ref, refOK := object["ref"].(string)
		digest, digestOK := object["digest"].(string)
		media, mediaOK := object["media_type"].(string)
		role, roleOK := object["role"].(string)
		if !refOK || ref == "" || utf8.RuneCountInString(ref) > 2048 || !digestOK || !digestPattern.MatchString(digest) || !mediaOK || media == "" || utf8.RuneCountInString(media) > 255 || !roleOK || (role != "primary" && role != "supporting" && role != "derived") {
			return nil, fmt.Errorf("%s.evidence[%d]: invalid evidence entry", path, index)
		}
		normalized := map[string]any{"ref": norm.NFC.String(ref), "digest": digest, "media_type": norm.NFC.String(media), "role": role}
		if redacted, found := object["redacted"]; found {
			boolean, ok := redacted.(bool)
			if !ok {
				return nil, fmt.Errorf("%s.evidence[%d].redacted: must be boolean", path, index)
			}
			normalized["redacted"] = boolean
		}
		if description, found := object["description"]; found {
			text, ok := description.(string)
			if !ok || utf8.RuneCountInString(text) > 512 {
				return nil, fmt.Errorf("%s.evidence[%d].description: invalid description", path, index)
			}
			normalized["description"] = norm.NFC.String(text)
		}
		result = append(result, normalized)
	}
	return result, nil
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
	hazards := map[string]struct{}{}
	var walk func(any, string)
	walk = func(current any, currentPath string) {
		switch item := current.(type) {
		case map[string]any:
			for key, child := range item {
				childPath := currentPath + "." + key
				if _, secretName := secretFieldNames[strings.ToLower(key)]; secretName {
					if text, ok := child.(string); ok && !looksRedactedOrIndirect(text) {
						hazards[childPath+": secret-like field value"] = struct{}{}
					}
				}
				walk(child, childPath)
			}
		case []any:
			for index, child := range item {
				walk(child, fmt.Sprintf("%s[%d]", currentPath, index))
			}
		case string:
			for _, candidate := range []struct {
				label   string
				pattern *regexp.Regexp
			}{{"private key material", privateKeyPattern}, {"bearer credential", bearerPattern}, {"JWT-like credential", jwtPattern}, {"GitHub token-like credential", githubTokenPattern}, {"AWS access-key-like credential", awsKeyPattern}} {
				if candidate.pattern.MatchString(item) {
					hazards[currentPath+": "+candidate.label] = struct{}{}
				}
			}
			if parsed, err := url.Parse(item); err == nil && parsed.Scheme != "" {
				if parsed.User != nil {
					hazards[currentPath+": credential-bearing URL userinfo"] = struct{}{}
				}
				for key, values := range parsed.Query() {
					if _, secretName := secretFieldNames[strings.ToLower(key)]; !secretName {
						continue
					}
					for _, value := range values {
						if !looksRedactedOrIndirect(value) {
							hazards[currentPath+": secret-like URL query parameter "+fmt.Sprintf("%q", key)] = struct{}{}
							break
						}
					}
				}
			}
		}
	}
	walk(value, path)
	result := make([]string, 0, len(hazards))
	for hazard := range hazards {
		result = append(result, hazard)
	}
	sort.Strings(result)
	return result
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
