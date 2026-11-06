package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

type SessionKey struct {
	ProjectKey string `json:"projectKey"`
	SessionID  string `json:"sessionId"`
	Subpath    string `json:"subpath,omitempty"`
}

// SessionStore adapters must honor context cancellation. Entries are native
// transcript JSON objects; append order must be preserved per key.
type SessionStore interface {
	Append(context.Context, SessionKey, []json.RawMessage) error
	Load(context.Context, SessionKey) ([]json.RawMessage, error)
}
type StoredSession struct {
	SessionID string
	Mtime     int64
}
type SessionLister interface {
	ListSessions(context.Context, string) ([]StoredSession, error)
}
type SessionDeleter interface {
	Delete(context.Context, SessionKey) error
}
type SessionSubkeyLister interface {
	ListSubkeys(context.Context, SessionKey) ([]string, error)
}

type InMemorySessionStore struct {
	mu      sync.Mutex
	entries map[SessionKey][]json.RawMessage
	mtimes  map[SessionKey]int64
	clock   int64
}

func NewInMemorySessionStore() *InMemorySessionStore { return &InMemorySessionStore{} }
func cloneEntries(entries []json.RawMessage) []json.RawMessage {
	if entries == nil {
		return nil
	}
	copy := make([]json.RawMessage, len(entries))
	for i, raw := range entries {
		copy[i] = bytes.Clone(raw)
	}
	return copy
}
func validateSessionKey(key SessionKey) error {
	if key.ProjectKey == "" || strings.ContainsAny(key.ProjectKey, "/\\") || key.ProjectKey == "." || key.ProjectKey == ".." || !uuidPattern.MatchString(key.SessionID) {
		return fmt.Errorf("claude: invalid session store key")
	}
	if key.Subpath != "" && (filepath.IsAbs(key.Subpath) || strings.Contains(key.Subpath, `\`) || strings.Contains(key.Subpath, ":") || strings.HasPrefix(filepath.ToSlash(filepath.Clean(key.Subpath)), "../") || filepath.Clean(key.Subpath) == "..") {
		return fmt.Errorf("claude: invalid session store subpath")
	}
	return nil
}
func (s *InMemorySessionStore) Append(ctx context.Context, key SessionKey, entries []json.RawMessage) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateSessionKey(key); err != nil {
		return err
	}
	for _, raw := range entries {
		var object map[string]any
		if json.Unmarshal(raw, &object) != nil || object == nil {
			return fmt.Errorf("claude: store entry must be an object")
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.entries == nil {
		s.entries = map[SessionKey][]json.RawMessage{}
		s.mtimes = map[SessionKey]int64{}
	}
	s.entries[key] = append(s.entries[key], cloneEntries(entries)...)
	now := time.Now().UnixMilli()
	if now <= s.clock {
		now = s.clock + 1
	}
	s.clock = now
	s.mtimes[key] = now
	return nil
}
func (s *InMemorySessionStore) Load(ctx context.Context, key SessionKey) ([]json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneEntries(s.entries[key]), nil
}
func (s *InMemorySessionStore) ListSessions(ctx context.Context, project string) ([]StoredSession, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var list []StoredSession
	for key := range s.entries {
		if key.ProjectKey == project && key.Subpath == "" {
			list = append(list, StoredSession{key.SessionID, s.mtimes[key]})
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Mtime > list[j].Mtime })
	return list, nil
}
func (s *InMemorySessionStore) Delete(ctx context.Context, key SessionKey) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for k := range s.entries {
		if k == key || key.Subpath == "" && k.ProjectKey == key.ProjectKey && k.SessionID == key.SessionID {
			delete(s.entries, k)
			delete(s.mtimes, k)
		}
	}
	return nil
}
func (s *InMemorySessionStore) ListSubkeys(ctx context.Context, key SessionKey) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var keys []string
	for k := range s.entries {
		if k.ProjectKey == key.ProjectKey && k.SessionID == key.SessionID && k.Subpath != "" {
			keys = append(keys, k.Subpath)
		}
	}
	sort.Strings(keys)
	return keys, nil
}

func sessionKey(id string, options SessionOptions) (SessionKey, error) {
	project, err := ProjectKeyForDirectory(options.Directory)
	key := SessionKey{ProjectKey: project, SessionID: id}
	if err == nil {
		err = validateSessionKey(key)
	}
	return key, err
}
func parseStoredEntries(raw []json.RawMessage) []transcriptEntry {
	var entries []transcriptEntry
	for _, data := range raw {
		var entry transcriptEntry
		if json.Unmarshal(data, &entry) == nil && entry != nil {
			entries = append(entries, entry)
		}
	}
	return entries
}
func listStoredSessions(ctx context.Context, o SessionOptions) ([]SessionInfo, error) {
	lister, ok := o.Store.(SessionLister)
	if !ok {
		return nil, fmt.Errorf("claude: store does not support listing")
	}
	project, err := ProjectKeyForDirectory(o.Directory)
	if err != nil {
		return nil, err
	}
	list, err := lister.ListSessions(ctx, project)
	if err != nil {
		return nil, err
	}
	var infos []SessionInfo
	for _, entry := range list {
		info, err := GetSessionInfo(ctx, entry.SessionID, o)
		if err != nil {
			return nil, err
		}
		if info != nil {
			info.LastModified = entry.Mtime
			infos = append(infos, *info)
		}
	}
	sort.SliceStable(infos, func(i, j int) bool { return infos[i].LastModified > infos[j].LastModified })
	return page(infos, o.Offset, o.Limit), nil
}
func listStoredSubagents(ctx context.Context, id string, o SessionOptions) ([]string, error) {
	lister, ok := o.Store.(SessionSubkeyLister)
	if !ok {
		return nil, fmt.Errorf("claude: store does not support subkey listing")
	}
	key, err := sessionKey(id, o)
	if err != nil {
		return nil, err
	}
	paths, err := lister.ListSubkeys(ctx, key)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, path := range paths {
		base := filepath.Base(path)
		if strings.HasPrefix(path, "subagents/") && strings.HasPrefix(base, "agent-") && strings.HasSuffix(base, ".jsonl") {
			ids = appendUnique(ids, strings.TrimSuffix(strings.TrimPrefix(base, "agent-"), ".jsonl"))
		}
	}
	sort.Strings(ids)
	return ids, nil
}

func FilePathToSessionKey(path, projectsDir string) (SessionKey, bool) {
	rel, err := filepath.Rel(projectsDir, path)
	if err != nil {
		return SessionKey{}, false
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) < 2 || parts[0] == ".." {
		return SessionKey{}, false
	}
	key := SessionKey{ProjectKey: parts[0]}
	if len(parts) == 2 && strings.HasSuffix(parts[1], ".jsonl") {
		key.SessionID = strings.TrimSuffix(parts[1], ".jsonl")
	} else if len(parts) >= 3 {
		key.SessionID = parts[1]
		key.Subpath = strings.Join(parts[2:], "/")
	} else {
		return key, false
	}
	return key, validateSessionKey(key) == nil
}

func (c *Client) mirror(raw json.RawMessage) error {
	if c.opts.SessionStore == nil {
		return nil
	}
	var frame struct {
		FilePath string            `json:"filePath"`
		Entries  []json.RawMessage `json:"entries"`
	}
	if err := json.Unmarshal(raw, &frame); err != nil {
		return err
	}
	root, err := configDirectory(SessionOptions{ConfigDir: c.opts.Env["CLAUDE_CONFIG_DIR"]})
	if err != nil {
		return err
	}
	key, ok := FilePathToSessionKey(frame.FilePath, filepath.Join(root, "projects"))
	if !ok {
		return nil
	}
	ctx, cancel := context.WithTimeout(c.ctx, c.opts.LoadTimeout)
	defer cancel()
	return c.opts.SessionStore.Append(ctx, key, frame.Entries)
}

// materializeStoreResume creates a private, temporary CLI config tree. It never
// overwrites the user's transcript files and removes copied auth on cleanup.
func materializeStoreResume(ctx context.Context, o Options) (Options, func(), error) {
	noop := func() {}
	if o.Transport != nil || o.SessionStore == nil || o.Resume == "" && !o.Continue {
		return o, noop, nil
	}
	ctx, cancel := context.WithTimeout(ctx, o.LoadTimeout)
	defer cancel()
	project, err := ProjectKeyForDirectory(o.Dir)
	if err != nil {
		return o, noop, err
	}
	id := o.Resume
	if id == "" {
		lister, ok := o.SessionStore.(SessionLister)
		if !ok {
			return o, noop, fmt.Errorf("claude: continue from store requires listing")
		}
		list, err := lister.ListSessions(ctx, project)
		if err != nil {
			return o, noop, err
		}
		sort.Slice(list, func(i, j int) bool { return list[i].Mtime > list[j].Mtime })
		if len(list) == 0 {
			return o, noop, nil
		}
		id = list[0].SessionID
	}
	if !uuidPattern.MatchString(id) {
		return o, noop, nil
	}
	key := SessionKey{ProjectKey: project, SessionID: id}
	entries, err := o.SessionStore.Load(ctx, key)
	if err != nil {
		return o, noop, err
	}
	if entries == nil {
		return o, noop, nil
	}
	dir, err := os.MkdirTemp("", "agentbridge-claude-resume-")
	if err != nil {
		return o, noop, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	write := func(path string, entries []json.RawMessage) error {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return err
		}
		var buf bytes.Buffer
		for _, raw := range entries {
			buf.Write(raw)
			buf.WriteByte('\n')
		}
		return os.WriteFile(path, buf.Bytes(), 0600)
	}
	projectDir := filepath.Join(dir, "projects", project)
	if err = write(filepath.Join(projectDir, id+".jsonl"), entries); err == nil {
		err = copyResumeAuth(ctx, dir, o.Env)
	}
	if err == nil {
		if lister, ok := o.SessionStore.(SessionSubkeyLister); ok {
			var paths []string
			paths, err = lister.ListSubkeys(ctx, key)
			for _, subpath := range paths {
				if err != nil {
					break
				}
				subkey := key
				subkey.Subpath = subpath
				if err = validateSessionKey(subkey); err != nil {
					break
				}
				var raw []json.RawMessage
				raw, err = o.SessionStore.Load(ctx, subkey)
				if err == nil {
					err = write(filepath.Join(projectDir, id, filepath.FromSlash(subpath)), raw)
				}
			}
		}
	}
	if err != nil {
		cleanup()
		return o, noop, err
	}
	env := map[string]string{}
	for k, v := range o.Env {
		env[k] = v
	}
	env["CLAUDE_CONFIG_DIR"] = dir
	o.Env = env
	o.Resume = id
	o.Continue = false
	return o, cleanup, nil
}

func copyResumeAuth(ctx context.Context, dst string, env map[string]string) error {
	get := func(key string) string {
		if value, ok := env[key]; ok {
			return value
		}
		return os.Getenv(key)
	}
	sourceOverride := get("CLAUDE_CONFIG_DIR")
	source, err := configDirectory(SessionOptions{ConfigDir: sourceOverride})
	if err != nil {
		return err
	}
	for _, name := range []string{".credentials.json", "settings.json", "cowork_settings.json"} {
		data, err := os.ReadFile(filepath.Join(source, name))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		var object map[string]json.RawMessage
		if json.Unmarshal(bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf}), &object) == nil && object != nil {
			if name == ".credentials.json" {
				var auth map[string]json.RawMessage
				if json.Unmarshal(object["claudeAiOauth"], &auth) == nil && auth != nil {
					delete(auth, "refreshToken")
					object["claudeAiOauth"], _ = json.Marshal(auth)
				}
			} else {
				delete(object, "enabledPlugins")
				delete(object, "extraKnownMarketplaces")
				var vars map[string]json.RawMessage
				if json.Unmarshal(object["env"], &vars) == nil && vars != nil {
					delete(vars, "CLAUDE_CONFIG_DIR")
					object["env"], _ = json.Marshal(vars)
				}
			}
			data, _ = json.Marshal(object)
		}
		if err = os.WriteFile(filepath.Join(dst, name), data, 0600); err != nil {
			return err
		}
	}
	if runtime.GOOS == "darwin" && sourceOverride == "" && get("ANTHROPIC_API_KEY") == "" && get("CLAUDE_CODE_OAUTH_TOKEN") == "" {
		// Do not log keychain output. Remove the refresh token before the private
		// child sees it, so it cannot consume the parent's single-use token.
		data, err := exec.CommandContext(ctx, "security", "find-generic-password", "-s", "Claude Code-credentials", "-w").Output()
		if err == nil {
			var object map[string]json.RawMessage
			if json.Unmarshal(data, &object) == nil && object != nil {
				var auth map[string]json.RawMessage
				if json.Unmarshal(object["claudeAiOauth"], &auth) == nil && auth != nil {
					delete(auth, "refreshToken")
					object["claudeAiOauth"], _ = json.Marshal(auth)
				}
				data, _ = json.Marshal(object)
				if err = os.WriteFile(filepath.Join(dst, ".credentials.json"), data, 0600); err != nil {
					return err
				}
			}
		}
	}
	claudeJSON := filepath.Join(source, ".claude.json")
	if sourceOverride == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		claudeJSON = filepath.Join(home, ".claude.json")
	}
	data, err := os.ReadFile(claudeJSON)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dst, ".claude.json"), data, 0600)
}
