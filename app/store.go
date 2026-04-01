package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/energye/energy/v3/ipc"
	"github.com/energye/lcl/lcl"
	"github.com/energye/lcl/tool/exec"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const maxLogEntries = 500

type releaseStore struct {
	mu     sync.Mutex
	logsMu sync.Mutex
	loaded bool
	config AppConfig
	logs   []LogEntry
}

var appStore = &releaseStore{}

func (m *releaseStore) ensureLoadedLocked() error {
	if m.loaded {
		return nil
	}
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	m.config = cfg
	m.loaded = true
	return nil
}

func (m *releaseStore) withState(browserID uint32, fn func() (*ActionResponse, error)) *ActionResponse {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.ensureLoadedLocked(); err != nil {
		message := err.Error()
		m.appendLog(browserID, "error", message)
		return &ActionResponse{
			OK:      false,
			Message: message,
		}
	}

	restoreLogger := setOperationLogger(func(level, message string) {
		m.appendLog(browserID, level, message)
	})
	defer restoreLogger()

	resp, err := fn()
	if err != nil {
		if resp == nil {
			resp = &ActionResponse{}
		}
		resp.OK = false
		if resp.Message == "" {
			resp.Message = err.Error()
		}
		m.appendLog(browserID, "error", resp.Message)
		_ = m.saveLocked()
	}

	state, stateErr := m.buildStateLocked()
	if stateErr != nil {
		if resp == nil {
			resp = &ActionResponse{}
		}
		resp.OK = false
		if resp.Message == "" {
			resp.Message = stateErr.Error()
		}
		m.appendLog(browserID, "error", stateErr.Error())
		_ = m.saveLocked()
	} else {
		if resp == nil {
			resp = &ActionResponse{OK: true}
		}
		resp.State = state
		emitEvent(browserID, "app-state", state)
	}
	return resp
}

func (m *releaseStore) runAsync(browserID uint32, action string, fn func() (*ActionResponse, error)) {
	go func() {
		response := m.withState(browserID, fn)
		emitEvent(browserID, "operation-finished", &OperationEvent{
			Action:  action,
			OK:      response != nil && response.OK,
			Message: responseMessage(response),
			Release: responseRelease(response),
		})
	}()
}

func (m *releaseStore) appendLog(browserID uint32, level, message string) {
	if strings.TrimSpace(message) == "" {
		return
	}
	entry := LogEntry{
		Time:    time.Now().Format("2006-01-02 15:04:05"),
		Level:   level,
		Message: message,
	}
	m.logsMu.Lock()
	m.logs = append(m.logs, entry)
	if len(m.logs) > maxLogEntries {
		m.logs = append([]LogEntry(nil), m.logs[len(m.logs)-maxLogEntries:]...)
	}
	m.logsMu.Unlock()
	if canEmitEvent() {
		emitEvent(browserID, "log-entry", entry)
	}
}

func (m *releaseStore) snapshotLogs() []LogEntry {
	m.logsMu.Lock()
	defer m.logsMu.Unlock()
	return append([]LogEntry(nil), m.logs...)
}

func (m *releaseStore) saveLocked() error {
	data, err := json.MarshalIndent(m.config, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configFilePath(), data, 0644)
}

func (m *releaseStore) buildStateLocked() (*AppState, error) {
	repoByName := make(map[string]*RepoConfig)
	moduleToRepo := make(map[string]string)
	scans := make(map[string]repoScan)
	stateRepos := make([]RepoState, 0, len(m.config.Repos))
	configChanged := false
	selectedRepoName := strings.TrimSpace(m.config.SelectedRepo)
	if selectedRepoName == "" && len(m.config.Repos) > 0 {
		candidateNames := make([]string, 0, len(m.config.Repos))
		for _, repo := range m.config.Repos {
			candidateNames = append(candidateNames, strings.TrimSpace(repo.Name))
		}
		sort.Strings(candidateNames)
		selectedRepoName = candidateNames[0]
	}

	for i := range m.config.Repos {
		repo := &m.config.Repos[i]
		originalName := repo.Name
		originalRemoteURL := repo.RemoteURL
		originalLocalDir := repo.LocalDir
		originalModulePath := repo.ModulePath
		originalReleaseBranch := repo.ReleaseBranch
		originalDependencies := append([]string(nil), repo.Dependencies...)
		normalizeRepo(repo)
		if repo.Name != originalName ||
			repo.RemoteURL != originalRemoteURL ||
			repo.LocalDir != originalLocalDir ||
			repo.ModulePath != originalModulePath ||
			repo.ReleaseBranch != originalReleaseBranch ||
			!equalStringSlices(repo.Dependencies, originalDependencies) {
			configChanged = true
		}
		repoByName[repo.Name] = repo
		scanOptions := repoScanOptions{
			includeGoMod:    true,
			includeStatus:   true,
			includeBranches: true,
			includeTags:     true,
			//includeBranches: repo.Name == selectedRepoName,
			//includeTags:     repo.Name == selectedRepoName,
		}
		scan := scanRepositoryWithOptions(*repo, scanOptions)
		scans[repo.Name] = scan
		if repo.ModulePath == "" && scan.ModulePath != "" {
			repo.ModulePath = scan.ModulePath
			configChanged = true
		}
		if repo.ReleaseBranch == "" && scan.CurrentBranch != "" {
			repo.ReleaseBranch = scan.CurrentBranch
			configChanged = true
		}
		if repo.ModulePath != "" {
			moduleToRepo[repo.ModulePath] = repo.Name
		}
	}

	for i := range m.config.Repos {
		repo := &m.config.Repos[i]
		scan := scans[repo.Name]
		detected := detectDependencies(scan.RequiredModule, moduleToRepo, repo.Name)
		previousDependencies := append([]string(nil), repo.Dependencies...)
		if repo.DependenciesManual {
			repo.Dependencies = filterDependencies(repo.Dependencies, repo.Name, repoByName)
		} else {
			repo.Dependencies = detected
		}
		if !equalStringSlices(previousDependencies, repo.Dependencies) {
			configChanged = true
		}
	}

	reverseGraph := buildReverseGraph(m.config.Repos)

	for _, repo := range m.config.Repos {
		scan := scans[repo.Name]
		downstreams := append([]string(nil), reverseGraph[repo.Name]...)
		sort.Strings(downstreams)
		cascadeTargets := collectCascadeTargets(repo.Name, reverseGraph)
		stateRepos = append(stateRepos, RepoState{
			Name:                repo.Name,
			RemoteURL:           repo.RemoteURL,
			LocalDir:            repo.LocalDir,
			ModulePath:          repo.ModulePath,
			ReleaseBranch:       repo.ReleaseBranch,
			Dependencies:        append([]string(nil), repo.Dependencies...),
			Downstreams:         downstreams,
			CascadeTargets:      cascadeTargets,
			Branches:            append([]string(nil), scan.Branches...),
			CurrentBranch:       scan.CurrentBranch,
			Tags:                append([]string(nil), scan.Tags...),
			LatestTag:           scan.LatestTag,
			Dirty:               scan.Dirty,
			UncommittedCount:    scan.UncommittedCount,
			UnpushedCommitCount: scan.UnpushedCommitCount,
			Exists:              scan.Exists,
			IsGitRepo:           scan.IsGitRepo,
			HasGoMod:            scan.HasGoMod,
			LastScanError:       scan.LastScanError,
			ReleaseReady:        scan.Exists && scan.IsGitRepo && scan.CurrentBranch != "" && scan.LastScanError == "",
			DependenciesLabel:   dependenciesLabel(repo.Dependencies),
		})
	}

	sort.Slice(stateRepos, func(i, j int) bool {
		return stateRepos[i].Name < stateRepos[j].Name
	})

	if m.config.SelectedRepo == "" && len(stateRepos) > 0 {
		m.config.SelectedRepo = stateRepos[0].Name
		configChanged = true
	}
	if m.config.SelectedRepo != "" {
		if _, ok := repoByName[m.config.SelectedRepo]; !ok {
			m.config.SelectedRepo = ""
			configChanged = true
		}
	}

	if configChanged {
		if err := m.saveLocked(); err != nil {
			return nil, err
		}
	}

	return &AppState{
		Repos:        stateRepos,
		SelectedRepo: m.config.SelectedRepo,
		Logs:         m.snapshotLogs(),
	}, nil
}

func loadConfig() (AppConfig, error) {
	path := configFilePath()
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			cfg := AppConfig{
				Repos: []RepoConfig{},
			}
			return cfg, nil
		}
		return AppConfig{}, err
	}

	if len(strings.TrimSpace(string(data))) == 0 {
		return AppConfig{
			Repos: []RepoConfig{},
		}, nil
	}

	var cfg AppConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return AppConfig{}, fmt.Errorf("读取 config.json 失败: %w", err)
	}
	if cfg.Repos == nil {
		cfg.Repos = []RepoConfig{}
	}
	for i := range cfg.Repos {
		normalizeRepo(&cfg.Repos[i])
	}
	return cfg, nil
}

func configFilePath() string {
	if wd, err := os.Getwd(); err == nil {
		path := filepath.Join(wd, "config.json")
		if _, statErr := os.Stat(path); statErr == nil {
			return path
		}
	}
	if appDir := exec.AppDir(); appDir != "" {
		path := filepath.Join(appDir, "config.json")
		if _, statErr := os.Stat(path); statErr == nil {
			return path
		}
	}
	if wd, err := os.Getwd(); err == nil {
		return filepath.Join(wd, "config.json")
	}
	return "config.json"
}

func normalizeRepo(repo *RepoConfig) {
	repo.Name = strings.TrimSpace(repo.Name)
	repo.RemoteURL = strings.TrimSpace(repo.RemoteURL)
	repo.LocalDir = strings.TrimSpace(repo.LocalDir)
	repo.ModulePath = strings.TrimSpace(repo.ModulePath)
	repo.ReleaseBranch = strings.TrimSpace(repo.ReleaseBranch)
	repo.Dependencies = uniqueStrings(repo.Dependencies)
}

func dependenciesLabel(dependencies []string) string {
	if len(dependencies) == 0 {
		return "无上游依赖"
	}
	return strings.Join(dependencies, ", ")
}

func emitEvent(browserID uint32, name string, data any) {
	lcl.RunOnMainThreadAsync(func(id uint32) {
		if browserID != 0 {
			ipc.EmitOptions(&ipc.OptionsEvent{
				BrowserId: browserID,
				Name:      name,
				Data:      data,
			})
			return
		}
		ipc.Emit(name, data)
	})
}

func responseMessage(response *ActionResponse) string {
	if response == nil {
		return ""
	}
	return response.Message
}

func responseRelease(response *ActionResponse) *ReleaseResult {
	if response == nil {
		return nil
	}
	return response.Release
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func equalStringSlices(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func filterDependencies(dependencies []string, self string, repoByName map[string]*RepoConfig) []string {
	filtered := make([]string, 0, len(dependencies))
	for _, dependency := range dependencies {
		dependency = strings.TrimSpace(dependency)
		if dependency == "" || dependency == self {
			continue
		}
		if _, ok := repoByName[dependency]; !ok {
			continue
		}
		filtered = append(filtered, dependency)
	}
	return uniqueStrings(filtered)
}

func detectDependencies(required map[string]string, moduleToRepo map[string]string, self string) []string {
	if len(required) == 0 {
		return nil
	}
	result := make([]string, 0, len(required))
	for modulePath := range required {
		repoName, ok := moduleToRepo[modulePath]
		if !ok || repoName == self {
			continue
		}
		result = append(result, repoName)
	}
	return uniqueStrings(result)
}

func buildReverseGraph(repos []RepoConfig) map[string][]string {
	graph := make(map[string][]string)
	for _, repo := range repos {
		if _, ok := graph[repo.Name]; !ok {
			graph[repo.Name] = nil
		}
		for _, dependency := range repo.Dependencies {
			graph[dependency] = append(graph[dependency], repo.Name)
		}
	}
	for key := range graph {
		graph[key] = uniqueStrings(graph[key])
	}
	return graph
}

func collectCascadeTargets(root string, reverseGraph map[string][]string) []string {
	if root == "" {
		return nil
	}
	visited := make(map[string]struct{})
	queue := append([]string(nil), reverseGraph[root]...)
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		if _, ok := visited[name]; ok {
			continue
		}
		visited[name] = struct{}{}
		queue = append(queue, reverseGraph[name]...)
	}
	result := make([]string, 0, len(visited))
	for name := range visited {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}
