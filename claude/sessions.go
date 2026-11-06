package claude

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf16"
)

// SessionOptions resolves CLI-owned transcripts, or an optional external store.
// ConfigDir defaults to CLAUDE_CONFIG_DIR and then ~/.claude. Directory is the
// original project directory, not the sanitized transcript storage directory.
type SessionOptions struct {
	IncludeWorktrees      *bool // nil follows upstream and includes sibling Git worktrees.
	IncludeSystemMessages bool
	ConfigDir             string
	Directory             string
	Limit                 int
	Offset                int
	Store                 SessionStore
}

type SessionInfo struct {
	SessionID    string `json:"sessionId"`
	Summary      string `json:"summary"`
	LastModified int64  `json:"lastModified"`
	FileSize     int64  `json:"fileSize"`
	CustomTitle  string `json:"customTitle,omitempty"`
	FirstPrompt  string `json:"firstPrompt,omitempty"`
	GitBranch    string `json:"gitBranch,omitempty"`
	Cwd          string `json:"cwd,omitempty"`
	Tag          string `json:"tag,omitempty"`
	CreatedAt    int64  `json:"createdAt,omitempty"`
}

type SessionMessage struct {
	Type            string          `json:"type"`
	UUID            string          `json:"uuid"`
	SessionID       string          `json:"session_id"`
	Message         json.RawMessage `json:"message"`
	ParentToolUseID *string         `json:"parent_tool_use_id"`
	ParentAgentID   string          `json:"parent_agent_id,omitempty"`
	Raw             json.RawMessage `json:"-"`
}
type ForkSessionOptions struct {
	SessionOptions
	UpToMessageID string
	Title         string
}
type ForkSessionResult struct {
	SessionID string `json:"sessionId"`
}

var uuidPattern = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var agentIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
var nonAlphanumeric = regexp.MustCompile(`[^a-zA-Z0-9]`)

// ProjectKeyForDirectory matches the CLI's sanitized project storage key,
// including its UTF-16 hash suffix for long directory names.
func ProjectKeyForDirectory(directory string) (string, error) {
	if directory == "" {
		var err error
		directory, err = os.Getwd()
		if err != nil {
			return "", err
		}
	}
	path, err := filepath.Abs(directory)
	if err != nil {
		return "", err
	}
	if resolved, e := filepath.EvalSymlinks(path); e == nil {
		path = resolved
	}
	key := nonAlphanumeric.ReplaceAllString(path, "-")
	if len(key) <= 200 {
		return key, nil
	}
	var hash int32
	for _, unit := range utf16.Encode([]rune(path)) {
		hash = hash*31 + int32(unit)
	}
	n := int64(hash)
	if n < 0 {
		n = -n
	}
	return key[:200] + "-" + strconv.FormatInt(n, 36), nil
}

func configDirectory(options SessionOptions) (string, error) {
	path := options.ConfigDir
	if path == "" {
		path = os.Getenv("CLAUDE_CONFIG_DIR")
	}
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, ".claude")
	}
	return filepath.Abs(path)
}

func sessionPaths(options SessionOptions) ([]string, error) {
	root, err := configDirectory(options)
	if err != nil {
		return nil, err
	}
	root = filepath.Join(root, "projects")
	if options.Directory != "" {
		key, err := ProjectKeyForDirectory(options.Directory)
		if err != nil {
			return nil, err
		}
		paths := []string{filepath.Join(root, key)}
		if options.IncludeWorktrees == nil || *options.IncludeWorktrees {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			data, err := exec.CommandContext(ctx, "git", "-C", options.Directory, "-c", "core.quotePath=false", "worktree", "list", "--porcelain", "-z").Output()
			if err == nil {
				for _, record := range strings.Split(string(data), "\x00") {
					if strings.HasPrefix(record, "worktree ") {
						directory := strings.TrimPrefix(record, "worktree ")
						if sibling, e := ProjectKeyForDirectory(directory); e == nil {
							paths = appendUnique(paths, filepath.Join(root, sibling))
						}
					}
				}
			}
		}
		return paths, nil
	}
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var result []string
	for _, entry := range entries {
		if entry.IsDir() && entry.Type()&os.ModeSymlink == 0 {
			result = append(result, filepath.Join(root, entry.Name()))
		}
	}
	return result, nil
}

func findSession(id string, options SessionOptions) (string, error) {
	if !uuidPattern.MatchString(id) {
		return "", fmt.Errorf("claude: invalid session UUID")
	}
	dirs, err := sessionPaths(options)
	if err != nil {
		return "", err
	}
	for _, dir := range dirs {
		path := filepath.Join(dir, id+".jsonl")
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("claude: transcript is not a regular file")
		}
		return path, nil
	}
	return "", os.ErrNotExist
}

type transcriptEntry map[string]json.RawMessage

func textField(entry transcriptEntry, key string) string {
	var s string
	_ = json.Unmarshal(entry[key], &s)
	return s
}
func boolField(entry transcriptEntry, key string) bool {
	var b bool
	_ = json.Unmarshal(entry[key], &b)
	return b
}
func decodeEntries(reader io.Reader) ([]transcriptEntry, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), 16<<20)
	var result []transcriptEntry
	for scanner.Scan() {
		var entry transcriptEntry
		if json.Unmarshal(scanner.Bytes(), &entry) == nil && entry != nil {
			result = append(result, entry)
		}
	}
	return result, scanner.Err()
}
func readEntries(ctx context.Context, id string, options SessionOptions) ([]transcriptEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if options.Store != nil {
		key, err := sessionKey(id, options)
		if err != nil {
			return nil, err
		}
		raw, err := options.Store.Load(ctx, key)
		if err != nil {
			return nil, err
		}
		return parseStoredEntries(raw), nil
	}
	path, err := findSession(id, options)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return decodeEntries(f)
}

func sessionMetadata(id string, entries []transcriptEntry) SessionInfo {
	info := SessionInfo{SessionID: id}
	var aiTitle string
	for _, entry := range entries {
		if title := textField(entry, "customTitle"); title != "" {
			info.CustomTitle = title
		}
		if title := textField(entry, "aiTitle"); title != "" {
			aiTitle = title
		}
		if textField(entry, "type") == "tag" {
			info.Tag = textField(entry, "tag")
		}
		if cwd := textField(entry, "cwd"); cwd != "" {
			info.Cwd = cwd
		}
		if branch := textField(entry, "gitBranch"); branch != "" {
			info.GitBranch = branch
		}
		if stamp, err := time.Parse(time.RFC3339Nano, textField(entry, "timestamp")); err == nil {
			ms := stamp.UnixMilli()
			if info.CreatedAt == 0 || ms < info.CreatedAt {
				info.CreatedAt = ms
			}
			if ms > info.LastModified {
				info.LastModified = ms
			}
		}
		if info.FirstPrompt == "" && textField(entry, "type") == "user" && !boolField(entry, "isMeta") && !boolField(entry, "isSidechain") {
			var m Message
			m.Message = &APIMessage{}
			_ = json.Unmarshal(entry["message"], m.Message)
			prompt := strings.TrimSpace(m.Text())
			if prompt != "" && !strings.HasPrefix(prompt, "<") && !strings.HasPrefix(prompt, "[Request interrupted by user") {
				if len([]rune(prompt)) > 200 {
					prompt = string([]rune(prompt)[:200]) + "…"
				}
				info.FirstPrompt = prompt
			}
		}
	}
	info.Summary = info.CustomTitle
	if info.Summary == "" {
		info.Summary = aiTitle
	}
	if info.Summary == "" {
		info.Summary = info.FirstPrompt
	}
	return info
}

func ListSessions(ctx context.Context, options SessionOptions) ([]SessionInfo, error) {
	if options.Offset < 0 || options.Limit < 0 {
		return nil, fmt.Errorf("claude: invalid session pagination")
	}
	if options.Store != nil {
		return listStoredSessions(ctx, options)
	}
	dirs, err := sessionPaths(options)
	if err != nil {
		return nil, err
	}
	var result []SessionInfo
	for _, dir := range dirs {
		files, err := os.ReadDir(dir)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, file := range files {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			id := strings.TrimSuffix(file.Name(), ".jsonl")
			if file.IsDir() || !strings.HasSuffix(file.Name(), ".jsonl") || !uuidPattern.MatchString(id) {
				continue
			}
			info, err := GetSessionInfo(ctx, id, SessionOptions{ConfigDir: options.ConfigDir, Directory: options.Directory})
			if err != nil {
				return nil, err
			}
			if info != nil {
				result = append(result, *info)
			}
		}
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].LastModified > result[j].LastModified })
	return page(result, options.Offset, options.Limit), nil
}

func GetSessionInfo(ctx context.Context, id string, options SessionOptions) (*SessionInfo, error) {
	if !uuidPattern.MatchString(id) {
		return nil, nil
	}
	entries, err := readEntries(ctx, id, options)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, nil
	}
	info := sessionMetadata(id, entries)
	if options.Store == nil {
		path, err := findSession(id, options)
		if err != nil {
			return nil, err
		}
		stat, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		info.FileSize = stat.Size()
		info.LastModified = stat.ModTime().UnixMilli()
	}
	return &info, nil
}

func conversationChain(entries []transcriptEntry) []transcriptEntry {
	byID := map[string]transcriptEntry{}
	positions := map[string]int{}
	parents := map[string]bool{}
	for i, e := range entries {
		if id := textField(e, "uuid"); id != "" {
			byID[id] = e
			positions[id] = i
			if p := textField(e, "parentUuid"); p != "" {
				parents[p] = true
			}
		}
	}
	var leaf transcriptEntry
	best := -1
	mainFound := false
	for _, terminal := range entries {
		if parents[textField(terminal, "uuid")] {
			continue
		}
		seen := map[string]bool{}
		e := terminal
		for e != nil {
			uid := textField(e, "uuid")
			if seen[uid] {
				break
			}
			seen[uid] = true
			kind := textField(e, "type")
			if kind == "user" || kind == "assistant" {
				main := !boolField(e, "isSidechain") && !boolField(e, "isMeta") && textField(e, "teamName") == ""
				index := positions[uid]
				if main && !mainFound || main == mainFound && index > best {
					leaf = e
					best = index
					mainFound = main
				}
				break
			}
			e = byID[textField(e, "parentUuid")]
		}
	}
	var chain []transcriptEntry
	seen := map[string]bool{}
	for e := leaf; e != nil; e = byID[textField(e, "parentUuid")] {
		id := textField(e, "uuid")
		if seen[id] {
			break
		}
		seen[id] = true
		chain = append(chain, e)
	}
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}
	return recoverParallelResults(entries, chain, seen)
}

func recoverParallelResults(entries, chain []transcriptEntry, seen map[string]bool) []transcriptEntry {
	// Restore parallel tool branches only when their tool_use ids belong to an
	// assistant response already on this chain; unrelated abandoned branches stay out.
	toolIDs := map[string]bool{}
	assistantIDs := map[string]bool{}
	blocks := func(e transcriptEntry) (APIMessage, []ContentBlock) {
		var m APIMessage
		_ = json.Unmarshal(e["message"], &m)
		var b []ContentBlock
		_ = json.Unmarshal(m.Content, &b)
		return m, b
	}
	for _, e := range chain {
		m, b := blocks(e)
		if m.Role == "assistant" {
			assistantIDs[m.ID] = true
			for _, block := range b {
				if block.Type == "tool_use" {
					toolIDs[block.ID] = true
				}
			}
		}
	}
	for _, e := range entries {
		if seen[textField(e, "uuid")] || boolField(e, "isSidechain") || textField(e, "teamName") != "" {
			continue
		}
		m, b := blocks(e)
		include := false
		if m.Role == "assistant" && m.ID != "" && assistantIDs[m.ID] {
			include = true
			for _, block := range b {
				if block.Type == "tool_use" {
					toolIDs[block.ID] = true
				}
			}
		}
		if m.Role == "user" {
			for _, block := range b {
				if block.Type == "tool_result" && toolIDs[block.ToolUseID] {
					include = true
				}
			}
		}
		if include {
			chain = append(chain, e)
			seen[textField(e, "uuid")] = true
		}
	}
	positions := map[string]int{}
	for i, e := range entries {
		positions[textField(e, "uuid")] = i
	}
	sort.SliceStable(chain, func(i, j int) bool {
		return positions[textField(chain[i], "uuid")] < positions[textField(chain[j], "uuid")]
	})
	return chain
}

func entriesMessages(entries []transcriptEntry, options SessionOptions) []SessionMessage {
	var result []SessionMessage
	for _, e := range conversationChain(entries) {
		kind := textField(e, "type")
		if (kind != "user" && kind != "assistant" && !(options.IncludeSystemMessages && kind == "system")) || boolField(e, "isMeta") || boolField(e, "isSidechain") || textField(e, "teamName") != "" {
			continue
		}
		raw, _ := json.Marshal(e)
		result = append(result, SessionMessage{Type: kind, UUID: textField(e, "uuid"), SessionID: textField(e, "sessionId"), Message: e["message"], Raw: raw})
	}
	return page(result, options.Offset, options.Limit)
}
func GetSessionMessages(ctx context.Context, id string, options SessionOptions) ([]SessionMessage, error) {
	if options.Offset < 0 || options.Limit < 0 {
		return nil, fmt.Errorf("claude: invalid session pagination")
	}
	if !uuidPattern.MatchString(id) {
		return nil, nil
	}
	entries, err := readEntries(ctx, id, options)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return entriesMessages(entries, options), nil
}
func page[T any](items []T, offset, limit int) []T {
	if offset >= len(items) {
		return nil
	}
	if offset > 0 {
		items = items[offset:]
	}
	if limit > 0 && limit < len(items) {
		items = items[:limit]
	}
	return items
}

func appendSession(ctx context.Context, id string, entry map[string]any, options SessionOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !uuidPattern.MatchString(id) {
		return fmt.Errorf("claude: invalid session UUID")
	}
	entry["sessionId"] = id
	raw, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	if options.Store != nil {
		key, err := sessionKey(id, options)
		if err != nil {
			return err
		}
		return options.Store.Append(ctx, key, []json.RawMessage{raw})
	}
	path, err := findSession(id, options)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.Write(append(raw, '\n'))
	return err
}
func RenameSession(ctx context.Context, id, title string, options SessionOptions) error {
	title = strings.TrimSpace(title)
	if title == "" {
		return fmt.Errorf("claude: title must not be empty")
	}
	return appendSession(ctx, id, map[string]any{"type": "custom-title", "customTitle": title}, options)
}
func TagSession(ctx context.Context, id string, tag *string, options SessionOptions) error {
	value := ""
	if tag != nil {
		value = strings.TrimSpace(strings.Map(func(r rune) rune {
			if unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Co) {
				return -1
			}
			return r
		}, *tag))
		if value == "" {
			return fmt.Errorf("claude: tag must not be empty; use nil to clear")
		}
	}
	return appendSession(ctx, id, map[string]any{"type": "tag", "tag": value}, options)
}
func DeleteSession(ctx context.Context, id string, options SessionOptions) error {
	if !uuidPattern.MatchString(id) {
		return fmt.Errorf("claude: invalid session UUID")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if options.Store != nil {
		d, ok := options.Store.(SessionDeleter)
		if !ok {
			return fmt.Errorf("claude: store does not support deletion")
		}
		key, err := sessionKey(id, options)
		if err != nil {
			return err
		}
		return d.Delete(ctx, key)
	}
	path, err := findSession(id, options)
	if err != nil {
		return err
	}
	return os.Remove(path)
}

func newUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	h := hex.EncodeToString(b[:])
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:], nil
}

func ForkSession(ctx context.Context, id string, options ForkSessionOptions) (*ForkSessionResult, error) {
	if !uuidPattern.MatchString(id) || options.UpToMessageID != "" && !uuidPattern.MatchString(options.UpToMessageID) {
		return nil, fmt.Errorf("claude: invalid fork UUID")
	}
	entries, err := readEntries(ctx, id, options.SessionOptions)
	if err != nil {
		return nil, err
	}
	mapping := map[string]string{}
	byID := map[string]transcriptEntry{}
	var transcript []transcriptEntry
	found := options.UpToMessageID == ""
	for _, e := range entries {
		if boolField(e, "isSidechain") {
			continue
		}
		uid := textField(e, "uuid")
		if uid == "" {
			continue
		}
		kind := textField(e, "type")
		if kind != "user" && kind != "assistant" && kind != "system" && kind != "progress" {
			continue
		}
		transcript = append(transcript, e)
		byID[uid] = e
		mapping[uid], err = newUUID()
		if err != nil {
			return nil, err
		}
		if uid == options.UpToMessageID {
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("claude: fork cutoff message not found")
	}
	if len(transcript) == 0 {
		return nil, fmt.Errorf("claude: session has no messages to fork")
	}
	forkID, err := newUUID()
	if err != nil {
		return nil, err
	}
	var output []json.RawMessage
	for _, e := range transcript {
		if textField(e, "type") == "progress" {
			continue
		}
		copy := transcriptEntry{}
		for key, value := range e {
			copy[key] = value
		}
		set := func(key string, value any) { copy[key], _ = json.Marshal(value) }
		uid := textField(e, "uuid")
		set("uuid", mapping[uid])
		set("sessionId", forkID)
		set("isSidechain", false)
		set("forkedFrom", map[string]any{"sessionId": id, "messageUuid": uid})
		parent := textField(e, "parentUuid")
		visited := map[string]bool{}
		for parent != "" && textField(byID[parent], "type") == "progress" && !visited[parent] {
			visited[parent] = true
			parent = textField(byID[parent], "parentUuid")
		}
		if mapped := mapping[parent]; mapped != "" {
			set("parentUuid", mapped)
		} else {
			set("parentUuid", nil)
		}
		if logical := textField(e, "logicalParentUuid"); logical != "" {
			if mapped := mapping[logical]; mapped != "" {
				set("logicalParentUuid", mapped)
			} else {
				set("logicalParentUuid", nil)
			}
		}
		for _, key := range []string{"teamName", "agentName", "slug", "sourceToolAssistantUUID"} {
			delete(copy, key)
		}
		raw, _ := json.Marshal(copy)
		output = append(output, raw)
	}
	if len(output) == 0 {
		return nil, fmt.Errorf("claude: session has no visible messages to fork")
	}
	var leaf transcriptEntry
	_ = json.Unmarshal(output[len(output)-1], &leaf)
	leaf["timestamp"], _ = json.Marshal(time.Now().UTC().Format(time.RFC3339Nano))
	output[len(output)-1], _ = json.Marshal(leaf)
	title := strings.TrimSpace(options.Title)
	if title == "" {
		title = sessionMetadata(id, entries).Summary
		if title == "" {
			title = "Forked session"
		}
		title += " (fork)"
	}
	metadata, _ := json.Marshal(map[string]any{"type": "custom-title", "sessionId": forkID, "customTitle": title})
	output = append(output, metadata)
	if options.Store != nil {
		key, err := sessionKey(forkID, options.SessionOptions)
		if err != nil {
			return nil, err
		}
		err = options.Store.Append(ctx, key, output)
	} else {
		source, e := findSession(id, options.SessionOptions)
		if e != nil {
			return nil, e
		}
		file, e := os.OpenFile(filepath.Join(filepath.Dir(source), forkID+".jsonl"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return nil, e
		}
		for _, raw := range output {
			if _, err = file.Write(append(raw, '\n')); err != nil {
				break
			}
		}
		closeErr := file.Close()
		if err == nil {
			err = closeErr
		}
	}
	if err != nil {
		return nil, err
	}
	return &ForkSessionResult{SessionID: forkID}, nil
}

func ListSubagents(ctx context.Context, id string, options SessionOptions) ([]string, error) {
	if options.Store != nil {
		return listStoredSubagents(ctx, id, options)
	}
	path, err := findSession(id, options)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	root := strings.TrimSuffix(path, ".jsonl") + string(filepath.Separator) + "subagents"
	var agents []string
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		name := entry.Name()
		if !entry.IsDir() && entry.Type()&os.ModeSymlink == 0 && strings.HasPrefix(name, "agent-") && strings.HasSuffix(name, ".jsonl") {
			agent := strings.TrimSuffix(strings.TrimPrefix(name, "agent-"), ".jsonl")
			if agentIDPattern.MatchString(agent) {
				agents = appendUnique(agents, agent)
			}
		}
		return nil
	})
	sort.Strings(agents)
	return agents, err
}
func GetSubagentMessages(ctx context.Context, id, agent string, options SessionOptions) ([]SessionMessage, error) {
	if !agentIDPattern.MatchString(agent) {
		return nil, fmt.Errorf("claude: invalid agent ID")
	}
	if options.Store != nil {
		key, err := sessionKey(id, options)
		if err != nil {
			return nil, err
		}
		key.Subpath = "subagents/agent-" + agent + ".jsonl"
		raw, err := options.Store.Load(ctx, key)
		if err != nil {
			return nil, err
		}
		return subagentMessages(parseStoredEntries(raw), agent, options), nil
	}
	path, err := findSession(id, options)
	if err != nil {
		return nil, err
	}
	root := strings.TrimSuffix(path, ".jsonl") + string(filepath.Separator) + "subagents"
	var entries []transcriptEntry
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if entry.Name() == "agent-"+agent+".jsonl" && entry.Type()&os.ModeSymlink == 0 {
			f, e := os.Open(path)
			if e != nil {
				return e
			}
			defer f.Close()
			entries, e = decodeEntries(f)
			return e
		}
		return ctx.Err()
	})
	if err != nil {
		return nil, err
	}
	return subagentMessages(entries, agent, options), nil
}
func subagentMessages(entries []transcriptEntry, agent string, options SessionOptions) []SessionMessage {
	copies := make([]transcriptEntry, 0, len(entries))
	for _, e := range entries {
		copy := transcriptEntry{}
		for k, v := range e {
			copy[k] = v
		}
		delete(copy, "isSidechain")
		copies = append(copies, copy)
	}
	messages := entriesMessages(copies, options)
	for i := range messages {
		messages[i].ParentAgentID = agent
	}
	return messages
}
