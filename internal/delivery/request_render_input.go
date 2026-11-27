// What the surfaces share when they take words from a caller: REST, the
// assistant tools, the command line and chat. Owner: part B.

package delivery

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"
	"time"
)

// SurfaceContent is what a creator may write. A request's key, receipt code,
// report, decline and operator words are datawatch's and the later calls'.
type SurfaceContent struct {
	Ask        string            `json:"ask,omitempty"`
	Detail     string            `json:"detail,omitempty"`
	Params     map[string]string `json:"params,omitempty"`
	Items      []DoneItem        `json:"items,omitempty"`
	ProjectDir string            `json:"project_dir,omitempty"`
}

// Content is the request content a creator's words make.
func (c SurfaceContent) Content() RequestContent {
	return RequestContent{Ask: c.Ask, Detail: c.Detail, Params: c.Params, Items: c.Items, ProjectDir: c.ProjectDir}
}

// surfaceKeyPrefix is the form of a key a creator names: it can never be one
// of the keys datawatch makes for itself.
const surfaceKeyPrefix = "operator:"

// SurfaceKey is the occurrence key of a create made through a surface. With no
// key the create is a new occurrence; a creator's own ID is kept under the
// operator form, so the same ID returns the same request.
func SurfaceKey(given string) (string, error) {
	id := strings.TrimPrefix(strings.TrimSpace(given), surfaceKeyPrefix)
	if id == "" {
		b := make([]byte, 16)
		if _, err := rand.Read(b); err != nil {
			return "", err
		}
		return surfaceKeyPrefix + hex.EncodeToString(b), nil
	}
	if CheckScopeID(id) != nil {
		return "", errors.Join(ErrRequestInput, errors.New("a key is 1 to 128 of letters, digits, dot, colon, dash and underscore"))
	}
	return surfaceKeyPrefix + id, nil
}

var safeRequestIDRe = regexp.MustCompile(`^r-[A-Za-z0-9_-]{1,64}$`)

// SafeRequestID is an ID as a log or audit line may hold it: only something
// shaped like a request ID is kept, so words sent in its place are not.
func SafeRequestID(id string) string {
	if safeRequestIDRe.MatchString(id) {
		return id
	}
	return "invalid"
}

// IsRequestID reports an ID of the older queue routes that names a request.
func IsRequestID(id string) bool { return strings.HasPrefix(id, RequestIDPrefix) }

// QueueClaimant is who an older queue claim is recorded for. It comes from the
// credential; what the call says about itself is not used.
func QueueClaimant(c Caller) string {
	if c.Kind == CallerSession {
		return c.SessionID
	}
	return c.Observed()
}

// QueueResultText keeps a queue completion's result as the report's words.
func QueueResultText(result map[string]any) string {
	if len(result) == 0 {
		return ""
	}
	b, err := json.Marshal(result)
	if err != nil {
		return fmt.Sprint(result)
	}
	return string(b)
}

// requestChatVerbs are the words after "request" that make a chat message a
// request command. Any other message that starts with the word is not one.
var requestChatVerbs = []string{"list", "show", "mine", "roles", "role", "create", "take", "accept", "report",
	"decline", "reissue", "drop", "force", VerbAcceptUnproven, "from-backlog"}

// RequestChatVerb returns the verb of a chat request command and the rest of
// the message. A bare "request" lists.
func RequestChatVerb(text string) (verb, rest string, ok bool) {
	fields := strings.Fields(text)
	if len(fields) == 0 || !strings.EqualFold(fields[0], "request") {
		return "", "", false
	}
	if len(fields) == 1 {
		return "list", "", true
	}
	verb = strings.ToLower(fields[1])
	if !slices.Contains(requestChatVerbs, verb) {
		return "", "", false
	}
	after := strings.TrimSpace(strings.TrimSpace(text)[len(fields[0]):])
	return verb, strings.TrimSpace(after[len(fields[1]):]), true
}

// SurfaceCreate builds a create from the words the command line, chat and the
// assistant tools take. With nothing else said it asks the supervisor of a
// project.
func SurfaceCreate(f map[string]string) (CreateInput, error) {
	role, scope, err := SurfaceScope(f)
	if err != nil {
		return CreateInput{}, err
	}
	content, err := surfaceContent(f)
	if err != nil {
		return CreateInput{}, err
	}
	in := CreateInput{
		Kind: RequestKind(orDefault(f["kind"], string(ReqAsk))), Role: role, Scope: scope, Host: f["host"],
		Source: SourceRef{Type: f["source_type"], ID: f["source_id"]}, After: f["after"], Content: content.Content(),
	}
	for name, into := range map[string]*time.Duration{"take_by": &in.TakeBy, "due": &in.Due} {
		if v := f[name]; v != "" {
			d, err := time.ParseDuration(v)
			if err != nil || d < 0 {
				return CreateInput{}, surfaceInput(name + " is a duration such as 30m or 24h")
			}
			*into = d
		}
	}
	if in.Key, err = SurfaceKey(f["key"]); err != nil {
		return CreateInput{}, err
	}
	return in, nil
}

// SurfaceScope reads role, scope_kind and scope_id.
func SurfaceScope(f map[string]string) (Role, Scope, error) {
	scope := Scope{Kind: ScopeKind(orDefault(f["scope_kind"], string(ScopeProject))), ID: f["scope_id"]}
	if scope.ID == "" {
		return "", Scope{}, surfaceInput("scope_id is required")
	}
	return Role(orDefault(f["role"], string(RoleSupervisor))), scope, nil
}

// SurfaceBacklog builds a request from a backlog entry.
func SurfaceBacklog(f map[string]string) (BacklogInput, error) {
	if f["entry_key"] == "" {
		return BacklogInput{}, surfaceInput("entry_key is required")
	}
	role, scope, err := SurfaceScope(f)
	if err != nil {
		return BacklogInput{}, err
	}
	content, err := surfaceContent(f)
	if err != nil {
		return BacklogInput{}, err
	}
	return BacklogInput{EntryKey: f["entry_key"], Role: role, Scope: scope, Content: content.Content()}, nil
}

func surfaceContent(f map[string]string) (SurfaceContent, error) {
	c := SurfaceContent{Ask: f["ask"], Detail: f["detail"], ProjectDir: f["project_dir"]}
	if v := f["params"]; v != "" {
		if err := surfaceJSON(v, &c.Params); err != nil {
			return c, surfaceInput("params is a JSON object of text values")
		}
	}
	if v := f["items"]; v != "" {
		if err := surfaceJSON(v, &c.Items); err != nil {
			return c, surfaceInput("items is a JSON array of done-when items with id, statement, from, method, path, contains, effect, change, before and target")
		}
	}
	return c, nil
}

// surfaceJSON refuses an unknown field: a command given to an item is refused
// and not dropped.
func surfaceJSON(text string, into any) error {
	dec := json.NewDecoder(strings.NewReader(text))
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		return err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return errors.New("one value only")
	}
	return nil
}

func surfaceInput(msg string) error { return errors.Join(ErrRequestInput, errors.New(msg)) }

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
