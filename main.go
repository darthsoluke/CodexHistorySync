package main

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "github.com/glebarez/go-sqlite"
)

const (
	toolNameEN = "CodexHistorySync"
	toolNameZH = "对话历史同步器"
)

var stateDBRE = regexp.MustCompile(`^state_(\d+)\.sqlite$`)

type stringList []string

func (s *stringList) String() string {
	return strings.Join(*s, ",")
}

func (s *stringList) Set(value string) error {
	if value == "" {
		return nil
	}
	*s = append(*s, value)
	return nil
}

func main() {
	os.Exit(run())
}

func run() int {
	var (
		codexHomeFlag string
		stateDBFlag   string
		providerFlag  string
		backupDirFlag string
		applyFlag     bool
		fromProviders stringList
		commandName   = filepath.Base(os.Args[0])
	)

	flag.StringVar(&codexHomeFlag, "codex-home", "", "Codex home directory. Defaults to the first detected local Codex home. / Codex 主目录。默认使用第一个检测到的本地 Codex 主目录。")
	flag.StringVar(&stateDBFlag, "state-db", "", "Explicit Codex state database path. / 指定 Codex 状态数据库路径。")
	flag.StringVar(&providerFlag, "provider", "", "Target provider name. Defaults to model_provider in config.toml. / 目标 provider 名称。默认使用 config.toml 里的 model_provider。")
	flag.Var(&fromProviders, "from-provider", "Only retag threads from these providers. Repeatable. Default: all providers except the target provider. / 只重标记这些 provider 的线程。可重复传入。默认：除目标 provider 外的所有 provider。")
	flag.StringVar(&backupDirFlag, "backup-dir", "", "Backup directory. Default: <codex-home>/backups/relink-<timestamp>. / 备份目录。默认：<codex-home>/backups/relink-<timestamp>。")
	flag.BoolVar(&applyFlag, "apply", false, "Write changes. Without this flag, the tool only prints a preview. / 写入更改。若不加此参数，只会预览。")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "%s (%s)\n\n", toolNameEN, toolNameZH)
		fmt.Fprintf(flag.CommandLine.Output(), "Usage / 用法: %s [flags]\n\n", commandName)
		fmt.Fprintln(flag.CommandLine.Output(), "Flags / 选项:")
		flag.PrintDefaults()
		fmt.Fprintln(flag.CommandLine.Output(), "\nExamples / 示例:")
		fmt.Fprintf(flag.CommandLine.Output(), "  %s --help\n", commandName)
		fmt.Fprintf(flag.CommandLine.Output(), "  %s --apply\n", commandName)
	}

	flag.Parse()

	codexHome, err := discoverCodexHome(codexHomeFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	targetProvider, err := loadTargetProvider(codexHome, providerFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	stateDB, err := discoverStateDB(codexHome, stateDBFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	backupRoot, err := buildBackupRoot(codexHome, backupDirFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	fromProviderList := dedupeStrings(fromProviders)
	if len(fromProviderList) == 0 {
		fromProviderList = nil
	}

	db, err := sql.Open("sqlite", stateDB)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer db.Close()

	if _, err := db.Exec("PRAGMA busy_timeout = 5000"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	if applyFlag {
		if err := backupStateDB(db, stateDB, backupRoot); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}

	providerCounts, err := countThreads(db, targetProvider, fromProviderList)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	threadCount := sumThreadCounts(providerCounts)

	files, err := rolloutFiles(codexHome)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	var fileChanges []fileChange
	for _, path := range files {
		changed, err := rewriteRolloutFile(path, codexHome, backupRoot, targetProvider, fromProviderList, applyFlag)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if changed > 0 {
			fileChanges = append(fileChanges, fileChange{path: path, changed: changed})
		}
	}

	var updatedThreads int64
	if applyFlag {
		tx, err := db.BeginTx(context.Background(), nil)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if _, err = updateThreads(tx, targetProvider, fromProviderList); err != nil {
			_ = tx.Rollback()
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		updatedThreads = threadCount
		if err := tx.Commit(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}

	printSummary(codexHome, stateDB, targetProvider, fromProviderList, backupRoot, providerCounts, files, fileChanges, !applyFlag, updatedThreads)
	return 0
}

func sumThreadCounts(providerCounts [][2]any) int64 {
	total := int64(0)
	for _, item := range providerCounts {
		if count, ok := item[1].(int64); ok {
			total += count
		}
	}
	return total
}

func dedupeStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func resolvePath(raw string) (string, error) {
	expanded, err := expandUser(raw)
	if err != nil {
		return "", err
	}
	if expanded == "" {
		return "", nil
	}
	if filepath.IsAbs(expanded) {
		return filepath.Clean(expanded), nil
	}
	return filepath.Abs(expanded)
}

func expandUser(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	if raw == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return home, nil
	}
	if strings.HasPrefix(raw, "~/") || strings.HasPrefix(raw, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, raw[2:]), nil
	}
	return raw, nil
}

func candidateCodexHomes() ([]string, error) {
	var candidates []string
	add := func(raw string) {
		if raw == "" {
			return
		}
		path, err := resolvePath(raw)
		if err != nil || path == "" {
			return
		}
		candidates = append(candidates, path)
	}

	add(os.Getenv("CODEX_HOME"))

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	add(filepath.Join(home, ".codex"))
	add(filepath.Join(home, ".config", "codex"))
	add(filepath.Join(home, ".local", "share", "codex"))
	add(filepath.Join(home, "Library", "Application Support", "Codex"))
	add(filepath.Join(home, "Library", "Application Support", ".codex"))

	for _, envVar := range []string{"APPDATA", "LOCALAPPDATA", "USERPROFILE"} {
		base := os.Getenv(envVar)
		if base == "" {
			continue
		}
		add(filepath.Join(base, ".codex"))
		add(filepath.Join(base, "Codex"))
	}

	return dedupePaths(candidates), nil
}

func dedupePaths(paths []string) []string {
	seen := make(map[string]struct{}, len(paths))
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		out = append(out, path)
	}
	return out
}

func scoreCodexHome(path string) int {
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return -1
	}

	score := 0
	if fileExists(filepath.Join(path, "config.toml")) {
		score += 8
	}
	if hasGlob(filepath.Join(path, "state_*.sqlite")) {
		score += 8
	}
	if dirExists(filepath.Join(path, "sessions")) {
		score += 2
	}
	if dirExists(filepath.Join(path, "archived_sessions")) {
		score += 2
	}
	if fileExists(filepath.Join(path, "history.jsonl")) {
		score += 1
	}
	if fileExists(filepath.Join(path, "session_index.jsonl")) {
		score += 1
	}

	return score
}

func discoverCodexHome(explicit string) (string, error) {
	if explicit != "" {
		return resolvePath(explicit)
	}

	candidates, err := candidateCodexHomes()
	if err != nil {
		return "", err
	}

	type scoredPath struct {
		path   string
		score  int
		length int
	}

	var scored []scoredPath
	for _, path := range candidates {
		score := scoreCodexHome(path)
		if score < 0 {
			continue
		}
		scored = append(scored, scoredPath{path: path, score: score, length: len(path)})
	}

	if len(scored) > 0 {
		sort.Slice(scored, func(i, j int) bool {
			if scored[i].score != scored[j].score {
				return scored[i].score > scored[j].score
			}
			if scored[i].length != scored[j].length {
				return scored[i].length < scored[j].length
			}
			return scored[i].path < scored[j].path
		})
		return scored[0].path, nil
	}

	tried := strings.Join(candidates, ", ")
	if tried != "" {
		return "", fmt.Errorf("could not find a Codex home directory automatically. Pass --codex-home explicitly. Tried: %s", tried)
	}
	return "", errors.New("could not find a Codex home directory automatically. Pass --codex-home explicitly.")
}

func loadTargetProvider(codexHome, provider string) (string, error) {
	if strings.TrimSpace(provider) != "" {
		return strings.TrimSpace(provider), nil
	}
	return readRootModelProvider(filepath.Join(codexHome, "config.toml"))
}

func readRootModelProvider(configPath string) (string, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return "", fmt.Errorf("missing config file: %s", configPath)
	}

	for _, rawLine := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(strings.SplitN(rawLine, "#", 2)[0])
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") {
			break
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(key) != "model_provider" {
			continue
		}

		literal := strings.TrimSpace(strings.TrimRight(value, ","))
		parsed, err := parseStringLiteral(literal)
		if err != nil {
			parsed = strings.Trim(literal, `"'`)
		}
		if strings.TrimSpace(parsed) != "" {
			return strings.TrimSpace(parsed), nil
		}
		break
	}

	return "", fmt.Errorf("could not find a root model_provider in %s", configPath)
}

func parseStringLiteral(raw string) (string, error) {
	if len(raw) < 2 {
		return "", errors.New("not a string literal")
	}
	if (raw[0] != '"' && raw[0] != '\'') || raw[len(raw)-1] != raw[0] {
		return "", errors.New("not a quoted literal")
	}
	return strconv.Unquote(raw)
}

func discoverStateDB(codexHome, explicit string) (string, error) {
	if explicit != "" {
		return resolvePath(explicit)
	}

	paths, err := filepath.Glob(filepath.Join(codexHome, "state_*.sqlite"))
	if err != nil {
		return "", err
	}
	if len(paths) == 0 {
		return "", fmt.Errorf("no state_*.sqlite file found under %s", codexHome)
	}

	type candidate struct {
		path    string
		version int
		modTime time.Time
	}

	best := candidate{version: -1}
	for _, path := range paths {
		base := filepath.Base(path)
		matches := stateDBRE.FindStringSubmatch(base)
		version := -1
		if len(matches) == 2 {
			if parsed, err := strconv.Atoi(matches[1]); err == nil {
				version = parsed
			}
		}

		info, err := os.Stat(path)
		if err != nil {
			continue
		}

		cur := candidate{path: path, version: version, modTime: info.ModTime()}
		if cur.version > best.version || (cur.version == best.version && cur.modTime.After(best.modTime)) {
			best = cur
		}
	}

	if best.path == "" {
		return "", fmt.Errorf("no usable state_*.sqlite file found under %s", codexHome)
	}
	return best.path, nil
}

func buildBackupRoot(codexHome, explicit string) (string, error) {
	if strings.TrimSpace(explicit) != "" {
		return resolvePath(explicit)
	}
	stamp := time.Now().Format("20060102-150405")
	return filepath.Join(codexHome, "backups", "relink-"+stamp), nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func hasGlob(pattern string) bool {
	matches, err := filepath.Glob(pattern)
	return err == nil && len(matches) > 0
}

func backupStateDB(db *sql.DB, stateDB, backupRoot string) error {
	backupPath := filepath.Join(backupRoot, filepath.Base(stateDB))
	if err := os.MkdirAll(filepath.Dir(backupPath), 0o755); err != nil {
		return err
	}
	_ = os.Remove(backupPath)

	if _, err := db.Exec("VACUUM INTO " + quoteSQLString(backupPath)); err == nil {
		return nil
	}

	return copySQLiteSnapshot(stateDB, backupPath)
}

func copySQLiteSnapshot(stateDB, backupPath string) error {
	if err := copyFile(backupPath, stateDB); err != nil {
		return err
	}

	for _, suffix := range []string{"-wal", "-shm"} {
		source := stateDB + suffix
		if !fileExists(source) {
			continue
		}
		if err := copyFile(backupPath+suffix, source); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(dst, src string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	info, err := in.Stat()
	if err != nil {
		return err
	}

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Chtimes(dst, info.ModTime(), info.ModTime())
}

func quoteSQLString(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

type fileChange struct {
	path    string
	changed int
}

func threadFilterClause(targetProvider string, fromProviders []string) (string, []any) {
	if len(fromProviders) > 0 {
		placeholders := make([]string, len(fromProviders))
		args := make([]any, 0, len(fromProviders))
		for i, provider := range fromProviders {
			placeholders[i] = "?"
			args = append(args, provider)
		}
		return "model_provider IN (" + strings.Join(placeholders, ",") + ")", args
	}
	return "model_provider != ?", []any{targetProvider}
}

func countThreads(db *sql.DB, targetProvider string, fromProviders []string) ([][2]any, error) {
	whereClause, args := threadFilterClause(targetProvider, fromProviders)
	query := "SELECT model_provider, COUNT(*) FROM threads WHERE " + whereClause + " GROUP BY model_provider ORDER BY COUNT(*) DESC, model_provider"

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var counts [][2]any
	for rows.Next() {
		var provider string
		var count int64
		if err := rows.Scan(&provider, &count); err != nil {
			return nil, err
		}
		counts = append(counts, [2]any{provider, count})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return counts, nil
}

func updateThreads(tx *sql.Tx, targetProvider string, fromProviders []string) (int64, error) {
	whereClause, args := threadFilterClause(targetProvider, fromProviders)
	query := "UPDATE threads SET model_provider = ? WHERE " + whereClause
	result, err := tx.Exec(query, append([]any{targetProvider}, args...)...)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func rolloutFiles(codexHome string) ([]string, error) {
	var files []string
	for _, root := range []string{filepath.Join(codexHome, "sessions"), filepath.Join(codexHome, "archived_sessions")} {
		info, err := os.Stat(root)
		if err != nil || !info.IsDir() {
			continue
		}
		err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if strings.HasPrefix(filepath.Base(path), "rollout-") && strings.HasSuffix(path, ".jsonl") {
				files = append(files, path)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(files)
	return files, nil
}

func rewriteRolloutFile(path, codexHome, backupRoot, targetProvider string, fromProviders []string, apply bool) (int, error) {
	lines, err := readLines(path)
	if err != nil {
		return 0, err
	}

	changed := 0
	newLines := make([]string, 0, len(lines))
	for _, rawLine := range lines {
		if strings.TrimSpace(rawLine) == "" {
			newLines = append(newLines, rawLine)
			continue
		}

		var record map[string]any
		if err := json.Unmarshal([]byte(rawLine), &record); err != nil {
			newLines = append(newLines, rawLine)
			continue
		}

		if recordType, ok := record["type"].(string); ok && recordType == "session_meta" {
			payload, ok := record["payload"].(map[string]any)
			if ok {
				currentProvider, _ := payload["model_provider"].(string)
				shouldRetag := currentProvider != targetProvider
				if len(fromProviders) > 0 {
					shouldRetag = shouldRetag && containsString(fromProviders, currentProvider)
				}
				if shouldRetag {
					payload["model_provider"] = targetProvider
					changed++
					encoded, err := compactJSON(record)
					if err != nil {
						return 0, err
					}
					rawLine = encoded
				}
			}
		}
		newLines = append(newLines, rawLine)
	}

	if changed > 0 && apply {
		if err := ensureBackup(codexHome, path, backupRoot); err != nil {
			return 0, err
		}
		if err := os.WriteFile(path, []byte(strings.Join(newLines, "\n")+"\n"), 0o644); err != nil {
			return 0, err
		}
	}

	return changed, nil
}

func readLines(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	reader := bufio.NewReader(file)
	var lines []string
	for {
		line, err := reader.ReadString('\n')
		if line != "" {
			line = strings.TrimSuffix(line, "\n")
			line = strings.TrimSuffix(line, "\r")
			lines = append(lines, line)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	return lines, nil
}

func compactJSON(value any) (string, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(value); err != nil {
		return "", err
	}
	return strings.TrimSpace(buf.String()), nil
}

func ensureBackup(parent, source, backupRoot string) error {
	rel, err := filepath.Rel(parent, source)
	if err != nil {
		return err
	}
	backupPath := filepath.Join(backupRoot, rel)
	return copyFile(backupPath, source)
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func printSummary(
	codexHome, stateDB, targetProvider string,
	fromProviders []string,
	backupRoot string,
	providerCounts [][2]any,
	files []string,
	fileChanges []fileChange,
	dryRun bool,
	updatedThreads int64,
) {
	totalThreads := int64(0)
	for _, item := range providerCounts {
		if count, ok := item[1].(int64); ok {
			totalThreads += count
		}
	}

	fmt.Printf("%s (%s)\n", toolNameEN, toolNameZH)
	fmt.Printf("Codex home: %s\n", codexHome)
	fmt.Printf("State DB:   %s\n", stateDB)
	fmt.Printf("Target:     %s\n", targetProvider)
	if len(fromProviders) > 0 {
		fmt.Printf("Sources:    %s\n", strings.Join(fromProviders, ", "))
	} else {
		fmt.Println("Sources:    all providers except the target")
	}
	fmt.Printf("Backup:     %s\n\n", backupRoot)

	fmt.Printf("Threads to retag (total): %d\n", totalThreads)
	if len(providerCounts) > 0 {
		fmt.Println("By provider:")
		for _, item := range providerCounts {
			fmt.Printf("  %s: %d\n", item[0], item[1])
		}
	} else {
		fmt.Println("By provider: none")
	}

	fmt.Printf("\nRollout files scanned: %d\n", len(files))
	fmt.Printf("Rollout files changed: %d\n", len(fileChanges))
	for i, change := range fileChanges {
		if i >= 10 {
			fmt.Printf("  ... %d more\n", len(fileChanges)-10)
			break
		}
		fmt.Printf("  %s: %d session_meta record(s)\n", change.path, change.changed)
	}

	if dryRun {
		fmt.Println("\nDry run only. Re-run with --apply to write changes.")
		return
	}

	totalChanged := int64(0)
	for _, change := range fileChanges {
		totalChanged += int64(change.changed)
	}

	fmt.Printf("\nUpdated threads: %d\n", updatedThreads)
	fmt.Printf("Updated session_meta records: %d\n", totalChanged)
	fmt.Printf("Backup written to: %s\n", backupRoot)
	fmt.Println("Restart Codex / reload VS Code to refresh the thread list.")
}
