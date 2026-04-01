package app

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"golang.org/x/mod/modfile"
	"io"
	"os"
	osExec "os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var semverTagPattern = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)([-+][0-9A-Za-z.\-]+)?$`)
var aheadCountPattern = regexp.MustCompile(`ahead (\d+)`)

type repoScanOptions struct {
	includeGoMod    bool
	includeBranches bool
	includeTags     bool
	includeStatus   bool
}

func scanRepository(repo RepoConfig) repoScan {
	return scanRepositoryWithOptions(repo, repoScanOptions{
		includeGoMod:    true,
		includeBranches: true,
		includeTags:     true,
		includeStatus:   true,
	})
}

func scanRepositoryWithOptions(repo RepoConfig, options repoScanOptions) repoScan {
	scan := repoScan{
		RequiredModule: make(map[string]string),
	}

	if repo.LocalDir == "" {
		scan.LastScanError = "未配置本地目录"
		return scan
	}

	info, err := os.Stat(repo.LocalDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			scan.LastScanError = "本地目录不存在"
			return scan
		}
		scan.LastScanError = err.Error()
		return scan
	}
	if !info.IsDir() {
		scan.LastScanError = "本地目录不是文件夹"
		return scan
	}
	scan.Exists = true

	if options.includeGoMod {
		goModPath := filepath.Join(repo.LocalDir, "go.mod")
		if _, err = os.Stat(goModPath); err == nil {
			scan.HasGoMod = true
			modulePath, required, parseErr := parseGoMod(goModPath)
			if parseErr != nil {
				scan.LastScanError = fmt.Sprintf("解析 go.mod 失败: %v", parseErr)
			} else {
				scan.ModulePath = modulePath
				scan.RequiredModule = required
			}
		}
	}

	output, err := probeCommand(repo.LocalDir, "git", "rev-parse", "--is-inside-work-tree")
	if err != nil || strings.TrimSpace(output) != "true" {
		if scan.LastScanError == "" {
			scan.LastScanError = "本地目录不是 git 仓库"
		}
		return scan
	}
	scan.IsGitRepo = true

	if options.includeStatus {
		if output, err = probeCommand(repo.LocalDir, "git", "status", "--porcelain=v1", "--branch"); err == nil {
			currentBranch, aheadCount, hasUpstream, dirtyCount := parseGitStatusSummary(output)
			scan.CurrentBranch = currentBranch
			scan.UncommittedCount = dirtyCount
			scan.Dirty = dirtyCount > 0
			if hasUpstream {
				scan.UnpushedCommitCount = aheadCount
			} else {
				scan.UnpushedCommitCount = countUnpushedCommits(repo.LocalDir, currentBranch)
			}
		}
	}
	if options.includeBranches {
		if output, err = probeCommand(repo.LocalDir, "git", "branch", "--format=%(refname:short)"); err == nil {
			scan.Branches = lines(output)
			if scan.CurrentBranch != "" && !contains(scan.Branches, scan.CurrentBranch) {
				scan.Branches = append(scan.Branches, scan.CurrentBranch)
				sort.Strings(scan.Branches)
			}
		}
	}
	if options.includeTags {
		if output, err = probeCommand(repo.LocalDir, "git", "tag", "--list", "--sort=-version:refname"); err == nil {
			scan.Tags = lines(output)
			if len(scan.Tags) > 0 {
				scan.LatestTag = scan.Tags[0]
			}
		}
	}

	return scan
}

func parseGitStatusSummary(output string) (string, int, bool, int) {
	if strings.TrimSpace(output) == "" {
		return "", 0, false, 0
	}

	currentBranch := ""
	unpushedCount := 0
	hasUpstream := false
	dirtyCount := 0
	normalized := strings.ReplaceAll(output, "\r\n", "\n")
	rawLines := strings.Split(normalized, "\n")
	for index, rawLine := range rawLines {
		if rawLine == "" {
			continue
		}
		if index == 0 && strings.HasPrefix(rawLine, "## ") {
			currentBranch, unpushedCount, hasUpstream = parseGitStatusHeader(strings.TrimSpace(strings.TrimPrefix(rawLine, "## ")))
			continue
		}
		if strings.TrimSpace(rawLine) != "" {
			dirtyCount++
		}
	}
	return currentBranch, unpushedCount, hasUpstream, dirtyCount
}

func parseGitStatusHeader(header string) (string, int, bool) {
	if strings.TrimSpace(header) == "" {
		return "", 0, false
	}

	currentBranch := strings.TrimSpace(header)
	hasUpstream := strings.Contains(header, "...")
	if index := strings.Index(header, "..."); index >= 0 {
		currentBranch = strings.TrimSpace(header[:index])
	} else if index := strings.Index(header, " "); index >= 0 {
		currentBranch = strings.TrimSpace(header[:index])
	}
	if currentBranch == "HEAD" {
		currentBranch = ""
	}

	unpushedCount := 0
	if matches := aheadCountPattern.FindStringSubmatch(header); len(matches) == 2 {
		if value, err := strconv.Atoi(matches[1]); err == nil && value >= 0 {
			unpushedCount = value
		}
	}
	return currentBranch, unpushedCount, hasUpstream
}

func parseGoMod(path string) (string, map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", nil, err
	}
	file, err := modfile.Parse(path, data, nil)
	if err != nil {
		return "", nil, err
	}
	required := make(map[string]string)
	for _, require := range file.Require {
		required[require.Mod.Path] = require.Mod.Version
	}
	modulePath := ""
	if file.Module != nil {
		modulePath = strings.TrimSpace(file.Module.Mod.Path)
	}
	return modulePath, required, nil
}

func (m *releaseStore) saveRepoLocked(payload RepoPayload) (string, error) {
	repo := RepoConfig{
		Name:               strings.TrimSpace(payload.Name),
		RemoteURL:          strings.TrimSpace(payload.RemoteURL),
		LocalDir:           strings.TrimSpace(payload.LocalDir),
		ModulePath:         strings.TrimSpace(payload.ModulePath),
		ReleaseBranch:      strings.TrimSpace(payload.ReleaseBranch),
		Dependencies:       uniqueStrings(payload.Dependencies),
		DependenciesManual: payload.DependenciesManual,
	}
	if repo.Name == "" {
		return "", errors.New("仓库名称不能为空")
	}

	replaced := false
	for i := range m.config.Repos {
		if m.config.Repos[i].Name == repo.Name {
			m.config.Repos[i] = repo
			replaced = true
			break
		}
	}
	if !replaced {
		m.config.Repos = append(m.config.Repos, repo)
	}
	m.config.SelectedRepo = repo.Name
	emitOperationLogf("info", "已保存仓库 %s", repo.Name)
	if err := m.saveLocked(); err != nil {
		return "", err
	}
	return fmt.Sprintf("仓库 %s 已保存", repo.Name), nil
}

func (m *releaseStore) deleteRepoLocked(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("缺少仓库名称")
	}
	index := -1
	for i := range m.config.Repos {
		if m.config.Repos[i].Name == name {
			index = i
			break
		}
	}
	if index < 0 {
		return "", fmt.Errorf("仓库 %s 不存在", name)
	}
	m.config.Repos = append(m.config.Repos[:index], m.config.Repos[index+1:]...)
	for i := range m.config.Repos {
		filtered := make([]string, 0, len(m.config.Repos[i].Dependencies))
		for _, dependency := range m.config.Repos[i].Dependencies {
			if dependency != name {
				filtered = append(filtered, dependency)
			}
		}
		m.config.Repos[i].Dependencies = uniqueStrings(filtered)
	}
	if m.config.SelectedRepo == name {
		m.config.SelectedRepo = ""
	}
	emitOperationLogf("info", "已删除仓库 %s", name)
	if err := m.saveLocked(); err != nil {
		return "", err
	}
	return fmt.Sprintf("仓库 %s 已删除", name), nil
}

func (m *releaseStore) selectRepoLocked(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		m.config.SelectedRepo = ""
		if err := m.saveLocked(); err != nil {
			return "", err
		}
		return "", nil
	}
	for _, repo := range m.config.Repos {
		if repo.Name == name {
			m.config.SelectedRepo = name
			if err := m.saveLocked(); err != nil {
				return "", err
			}
			return fmt.Sprintf("已切换到仓库 %s", name), nil
		}
	}
	return "", fmt.Errorf("仓库 %s 不存在", name)
}

func (m *releaseStore) refreshLocked() (string, error) {
	emitOperationLog("info", "已刷新仓库状态")
	return "仓库状态已刷新", nil
}

func (m *releaseStore) deleteTagLocked(payload DeleteTagPayload) (string, error) {
	repoName := strings.TrimSpace(payload.RepoName)
	tag := strings.TrimSpace(payload.Tag)
	if repoName == "" {
		return "", errors.New("请选择仓库")
	}
	if tag == "" {
		return "", errors.New("请选择要删除的标签")
	}

	var repo RepoConfig
	found := false
	for _, item := range m.config.Repos {
		if item.Name == repoName {
			repo = item
			found = true
			break
		}
	}
	if !found {
		return "", fmt.Errorf("仓库 %s 不存在", repoName)
	}

	scan := scanRepositoryWithOptions(repo, repoScanOptions{
		includeTags: true,
	})
	if !scan.Exists || !scan.IsGitRepo {
		return "", fmt.Errorf("%s 不是可操作的本地 git 仓库", repoName)
	}
	if !contains(scan.Tags, tag) {
		return "", fmt.Errorf("%s 不存在标签 %s", repoName, tag)
	}

	emitOperationLogf("info", "开始删除标签 %s -> %s", repoName, tag)
	if err := deleteGitTag(repo.LocalDir, tag); err != nil {
		return "", err
	}

	deletedRemote := false
	if payload.DeleteRemote {
		if err := deleteRemoteGitTag(repo.LocalDir, tag); err != nil {
			return "", err
		}
		deletedRemote = true
	}

	message := fmt.Sprintf("仓库 %s 已删除标签 %s", repoName, tag)
	if deletedRemote {
		message += "，并同步删除 origin 远端标签"
	}
	emitOperationLog("info", message)
	return message, nil
}

// executeReleaseLocked 执行受锁保护的发布流程，处理主仓库及其依赖仓库的标签创建和推送
// 该函数会：
// 1. 验证输入参数（仓库名称和标签）
// 2. 构建仓库依赖关系图并计算各仓库的发布层级
// 3. 首先发布根仓库，创建指定版本的标签
// 4. 按依赖层级依次发布下级仓库，自动升级版本号并更新依赖引用
// 5. 记录所有发布步骤并返回发布结果
//
// 参数说明:
//   - payload: 发布请求的有效载荷，包含仓库名、标签、推送配置等信息
//
// 返回值说明:
//   - *ReleaseResult: 发布操作的结果，包含处理的仓库列表和各步骤详情
//   - string: 发布摘要信息，描述发布的仓库和版本
//   - error: 执行过程中的错误，如果成功则为 nil
func (m *releaseStore) executeReleaseLocked(payload ReleasePayload) (*ReleaseResult, string, error) {
	repoName := strings.TrimSpace(payload.RepoName)
	tag := strings.TrimSpace(payload.Tag)
	if repoName == "" {
		return nil, "", errors.New("请选择要发布的仓库")
	}
	if err := validateTag(tag); err != nil {
		return nil, "", err
	}
	// 构建仓库配置索引和扫描结果缓存，用于后续快速查找
	repoByName := make(map[string]RepoConfig)
	scans := make(map[string]repoScan)
	modulePathByRepo := make(map[string]string)
	moduleRepoByPath := make(map[string]string)
	for _, repo := range m.config.Repos {
		repoByName[repo.Name] = repo
		scan := scanRepository(repo)
		scans[repo.Name] = scan
		modulePath := repo.ModulePath
		if modulePath == "" {
			modulePath = scan.ModulePath
		}
		if modulePath != "" {
			modulePathByRepo[repo.Name] = modulePath
			moduleRepoByPath[modulePath] = repo.Name
		}
	}
	// 验证根仓库存在性和可发布性，检查是否为本地 git 仓库
	rootRepo, ok := repoByName[repoName]
	if !ok {
		return nil, "", fmt.Errorf("仓库 %s 不存在", repoName)
	}
	// 验证依赖关系图的完整性，确保没有循环依赖等问题
	if err := validateDependencyGraph(m.config.Repos); err != nil {
		return nil, "", err
	}

	rootScan := scans[repoName]
	if !rootScan.Exists || !rootScan.IsGitRepo {
		return nil, "", fmt.Errorf("%s 不是可发布的本地 git 仓库", repoName)
	}
	reuseExistingRootTag := contains(rootScan.Tags, tag)
	// 构建反向依赖图并计算各仓库相对于根仓库的发布层级深度
	reverseGraph := buildReverseGraph(m.config.Repos)
	depths := computeDepths(repoName, reverseGraph)
	result := &ReleaseResult{
		RootRepo: repoName,
		RootTag:  tag,
	}
	// 记录已变更的版本信息，初始时只有根仓库使用指定的标签版本
	changedVersions := map[string]string{
		repoName: tag,
	}
	// 执行根仓库的发布操作，创建指定版本的标签
	rootStep, err := executeRootRelease(rootRepo, rootScan, tag, payload.PushRemote, reuseExistingRootTag)
	if err != nil {
		return nil, "", err
	}
	result.Steps = append(result.Steps, rootStep)
	emitOperationLog("info", rootStep.Message)

	maxDepth := 0
	for _, depth := range depths {
		if depth > maxDepth {
			maxDepth = depth
		}
	}
	// 按依赖层级从浅到深依次处理下级仓库的发布
	for level := 1; level <= maxDepth; level++ {
		// 收集当前层级的所有仓库并按名称排序，确保发布顺序稳定
		levelRepos := make([]string, 0)
		for name, depth := range depths {
			if depth == level {
				levelRepos = append(levelRepos, name)
			}
		}
		sort.Strings(levelRepos)
		for _, name := range levelRepos {
			repo := repoByName[name]
			scan := scans[name]
			if !scan.Exists || !scan.IsGitRepo {
				return result, "", fmt.Errorf("%s 不是可发布的本地 git 仓库", name)
			}
			// 收集当前仓库需要更新的依赖模块版本变化
			versionUpdates := collectVersionUpdates(repo, scan, changedVersions, moduleRepoByPath)
			if len(versionUpdates) == 0 {
				continue
			}
			// 基于最新标签推导下一个补丁版本号作为新标签
			newTag, err := nextPatchTag(scan.LatestTag)
			if err != nil {
				return result, "", fmt.Errorf("%s latest tag %s cannot derive next release tag: %w", name, scan.LatestTag, err)
			}
			// 检查新标签是否已存在，避免重复创建
			if contains(scan.Tags, newTag) {
				return result, "", fmt.Errorf("%s 已存在标签 %s，无法为同步后的提交重新创建同名标签", name, newTag)
			}
			// 执行依赖仓库的发布，更新 go.mod 中的依赖版本并创建新标签
			step, err := executeDependentRelease(repo, scan, newTag, versionUpdates, modulePathByRepo, payload.PushRemote)
			if err != nil {
				return result, "", err
			}
			// 记录当前仓库的新版本，供后续层级仓库引用
			result.Steps = append(result.Steps, step)
			changedVersions[name] = newTag
			emitOperationLog("info", step.Message)
		}
	}

	summary := fmt.Sprintf("发布完成: %s -> %s，共处理 %d 个仓库", repoName, tag, len(result.Steps))
	emitOperationLog("info", summary)
	return result, summary, nil
}

func collectVersionUpdates(repo RepoConfig, scan repoScan, changedVersions map[string]string, moduleRepoByPath map[string]string) map[string]string {
	updates := make(map[string]string)

	for _, dependency := range repo.Dependencies {
		if version, ok := changedVersions[dependency]; ok {
			updates[dependency] = version
		}
	}

	for modulePath := range scan.RequiredModule {
		repoName, ok := moduleRepoByPath[modulePath]
		if !ok || repoName == repo.Name {
			continue
		}
		version, ok := changedVersions[repoName]
		if !ok {
			continue
		}
		updates[repoName] = version
	}

	if len(updates) == 0 {
		return nil
	}
	return updates
}

func executeRootRelease(repo RepoConfig, scan repoScan, tag string, pushRemote bool, reuseExistingTag bool) (ReleaseStep, error) {
	if reuseExistingTag {
		pushed := false
		if pushRemote {
			if err := pushGitTag(repo.LocalDir, tag); err != nil {
				return ReleaseStep{}, fmt.Errorf("%s 推送已有标签 %s 到 origin 失败: %w", repo.Name, tag, err)
			}
			pushed = true
		}
		message := fmt.Sprintf("%s 复用已有标签 %s 作为下游升级基准", repo.Name, tag)
		if pushed {
			message += "，并已推送到 origin"
		}
		return ReleaseStep{
			RepoName:    repo.Name,
			Branch:      scan.CurrentBranch,
			PreviousTag: scan.LatestTag,
			NewTag:      tag,
			CreatedTag:  false,
			Pushed:      pushed,
			Message:     message,
		}, nil
	}

	branch, err := ensureReleaseBranch(repo, scan)
	if err != nil {
		return ReleaseStep{}, err
	}
	if scan.Dirty {
		emitOperationLogf("warn", "%s 当前存在未提交改动，继续创建标签并推送，但这些未提交内容不会包含在新标签中", repo.Name)
	}
	if err := createGitTag(repo.LocalDir, tag); err != nil {
		return ReleaseStep{}, fmt.Errorf("%s 创建根标签 %s 失败: %w", repo.Name, tag, err)
	}
	pushed := false
	if pushRemote {
		if err := pushGitTag(repo.LocalDir, tag); err != nil {
			return ReleaseStep{}, fmt.Errorf("%s 推送根标签 %s 到 origin 失败: %w", repo.Name, tag, err)
		}
		pushed = true
	}
	message := fmt.Sprintf("%s 已创建标签 %s", repo.Name, tag)
	if pushed {
		message += " 并推送到 origin"
	}
	return ReleaseStep{
		RepoName:    repo.Name,
		Branch:      branch,
		PreviousTag: scan.LatestTag,
		NewTag:      tag,
		CreatedTag:  true,
		Pushed:      pushed,
		Message:     message,
	}, nil
}

// executeDependentRelease 执行依赖仓库的发布流程，更新 go.mod 中的依赖版本并创建新标签
// 该函数用于处理有依赖关系的下级仓库发布，主要步骤包括：
// 1. 确保发布分支存在并切换到该分支
// 2. 更新 go.mod 文件中引用的依赖模块版本
// 3. 提交变更文件并创建新的版本标签
// 4. 可选地将分支和标签推送到远程仓库
//
// 参数说明:
//   - repo: 仓库配置信息，包含名称、本地路径等
//   - scan: 仓库扫描结果，包含当前标签、分支状态等信息
//   - tag: 要创建的新版本标签
//   - versionUpdates: 需要更新的模块版本映射，key 为模块路径，value 为目标版本
//   - modulePathByRepo: 仓库名到模块路径的映射关系
//   - pushRemote: 是否将发布结果推送到远程仓库
//
// 返回值说明:
//   - ReleaseStep: 发布步骤详情，包含操作的仓库、版本变化、推送状态等
//   - error: 执行过程中的错误，如果成功则为 nil
func executeDependentRelease(repo RepoConfig, scan repoScan, tag string, versionUpdates map[string]string, modulePathByRepo map[string]string, pushRemote bool) (ReleaseStep, error) {
	branch, err := ensureReleaseBranch(repo, scan)
	if err != nil {
		return ReleaseStep{}, err
	}
	// 检查仓库是否存在未提交的改动，发出警告提示
	if scan.Dirty {
		emitOperationLogf("warn", "%s 当前存在未提交改动，发布提交将只包含 go.mod/go.sum，其它本地改动保持不变", repo.Name)
	}
	// 批量更新 go.mod 中的依赖版本号，返回实际被修改的模块列表
	updates, err := updateGoModDependencies(repo.LocalDir, versionUpdates, modulePathByRepo)
	if err != nil {
		return ReleaseStep{}, fmt.Errorf("%s 更新 go.mod 失败: %w", repo.Name, err)
	}
	// 如果没有可更新的依赖版本，说明目标版本已经是最新，无需继续发布
	if len(updates) == 0 {
		return ReleaseStep{}, fmt.Errorf("%s 的 go.mod 没有可更新的依赖版本，目标版本: %s", repo.Name, formatVersionUpdates(versionUpdates))
	}
	emitOperationLogf("info", "%s 已按依赖链路写入 go.mod 版本: %s", repo.Name, formatDependencyUpdates(updates))
	// 暂存 go.mod 和 go.sum 文件到 git 索引区
	if err := gitAddReleaseFiles(repo.LocalDir); err != nil {
		return ReleaseStep{}, fmt.Errorf("%s 暂存发布文件失败: %w", repo.Name, err)
	}
	// 创建发布提交，提交信息包含仓库名、版本号和更新的依赖列表
	if err := commitReleaseChanges(repo.LocalDir, repo.Name, tag, updates); err != nil {
		return ReleaseStep{}, err
	}
	// 在当前提交上创建新的版本标签
	if err := createGitTag(repo.LocalDir, tag); err != nil {
		return ReleaseStep{}, fmt.Errorf("%s 创建下游标签 %s 失败: %w", repo.Name, tag, err)
	}
	// 如果需要推送远程，则原子性地推送分支和标签到 origin
	pushed := false
	if pushRemote {
		if err := pushBranchAndTagAtomic(repo.LocalDir, branch, tag); err != nil {
			return ReleaseStep{}, fmt.Errorf("%s 推送分支 %s 与标签 %s 到 origin 失败: %w", repo.Name, branch, tag, err)
		}
		pushed = true
	}
	// 构建发布步骤的结果信息，描述本次发布的操作内容
	message := fmt.Sprintf("%s 已升级依赖并发布 %s", repo.Name, tag)
	if pushed {
		message += "，提交与标签已推送"
	}
	return ReleaseStep{
		RepoName:            repo.Name,
		Branch:              branch,
		PreviousTag:         scan.LatestTag,
		NewTag:              tag,
		UpdatedDependencies: updates,
		CreatedCommit:       true,
		CreatedTag:          true,
		Pushed:              pushed,
		Message:             message,
	}, nil
}

func formatVersionUpdates(versionUpdates map[string]string) string {
	if len(versionUpdates) == 0 {
		return "无"
	}
	parts := make([]string, 0, len(versionUpdates))
	for repoName, version := range versionUpdates {
		parts = append(parts, fmt.Sprintf("%s=%s", repoName, version))
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}

func formatDependencyUpdates(updates []DependencyUpdate) string {
	if len(updates) == 0 {
		return "无"
	}
	parts := make([]string, 0, len(updates))
	for _, update := range updates {
		parts = append(parts, fmt.Sprintf("%s:%s->%s", update.RepoName, update.FromVersion, update.ToVersion))
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}

func ensureReleaseBranch(repo RepoConfig, scan repoScan) (string, error) {
	targetBranch := strings.TrimSpace(repo.ReleaseBranch)
	currentBranch := strings.TrimSpace(scan.CurrentBranch)
	if targetBranch == "" {
		targetBranch = currentBranch
	}
	if targetBranch == "" {
		return "", fmt.Errorf("%s 没有可用的发布分支", repo.Name)
	}
	// 如果需要切换分支
	if currentBranch != targetBranch {
		if scan.Dirty {
			emitOperationLogf("warn", "%s 当前分支 %s 与发布分支 %s 不一致，且存在未提交改动，继续在当前分支发布", repo.Name, currentBranch, targetBranch)
			return currentBranch, nil
		}
		if _, err := runCommand(repo.LocalDir, "git", "checkout", targetBranch); err != nil {
			return "", fmt.Errorf("%s 切换分支失败：%w", repo.Name, err)
		}
	}
	// 切换到目标分支后，拉取最新代码
	emitOperationLogf("info", "正在从 origin 拉取 %s 分支的最新提交...", targetBranch)
	if _, err := runCommand(repo.LocalDir, "git", "pull", "origin", targetBranch); err != nil {
		return "", fmt.Errorf("%s 拉取远端分支失败：%w", repo.Name, err)
	}
	return targetBranch, nil
}

// updateGoModDependencies 更新指定目录的 go.mod 文件中的依赖模块版本
// 该函数会遍历需要更新的模块版本列表，查找并替换 go.mod 中对应的依赖版本号
// 如果有实际变更，则格式化并写回 go.mod 文件
//
// 参数说明:
//   - dir: 包含 go.mod 文件的目录路径
//   - versionUpdates: 需要更新的仓库版本映射，key 为仓库名，value 为目标版本号
//   - modulePathByRepo: 仓库名到 Go 模块路径的映射关系
//
// 返回值说明:
//   - []DependencyUpdate: 实际发生的依赖更新列表，包含模块路径和版本变化信息
//   - error: 执行过程中的错误，如果没有实际更新则返回 nil, nil
func updateGoModDependencies(dir string, versionUpdates map[string]string, modulePathByRepo map[string]string) ([]DependencyUpdate, error) {
	// 解析 go.mod 文件内容为可操作的对象结构
	path := filepath.Join(dir, "go.mod")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取 go.mod 失败: %w", err)
	}
	file, err := modfile.Parse(path, data, nil)
	if err != nil {
		return nil, fmt.Errorf("解析 go.mod 失败: %w", err)
	}
	// 遍历所有需要更新的依赖，逐个检查并更新版本号
	updates := make([]DependencyUpdate, 0, len(versionUpdates))
	changed := false
	for repoName, newVersion := range versionUpdates {
		// 获取当前仓库对应的模块路径，确保不会更新空路径
		modulePath := strings.TrimSpace(modulePathByRepo[repoName])
		if modulePath == "" {
			return nil, fmt.Errorf("%s 缺少模块路径，无法更新 go.mod", repoName)
		}
		// 在 go.mod 的 require 列表中查找当前模块的现有版本号
		oldVersion := ""
		for _, require := range file.Require {
			if require.Mod.Path == modulePath {
				oldVersion = require.Mod.Version
				break
			}
		}
		// 如果目标版本与现有版本相同，跳过此依赖
		if oldVersion == newVersion {
			continue
		}
		// 添加或更新 require 声明，将模块版本升级到目标版本
		if err := file.AddRequire(modulePath, newVersion); err != nil {
			return nil, fmt.Errorf("更新依赖 %s 失败: %w", modulePath, err)
		}
		// 记录本次更新操作的详细信息
		updates = append(updates, DependencyUpdate{
			RepoName:    repoName,
			ModulePath:  modulePath,
			FromVersion: oldVersion,
			ToVersion:   newVersion,
		})
		changed = true
	}
	// 如果没有任何版本变更，直接返回空结果
	if !changed {
		return nil, nil
	}

	// 将修改后的 go.mod 对象格式化为标准文本并写回文件
	formatted, err := file.Format()
	if err != nil {
		return nil, fmt.Errorf("格式化 go.mod 失败: %w", err)
	}
	if err := os.WriteFile(path, formatted, 0644); err != nil {
		return nil, fmt.Errorf("写入 go.mod 失败: %w", err)
	}

	// 执行 go mod tidy 以同步 go.sum 文件并解析间接依赖
	// 这会下载新版本所需的依赖、更新校验和、清理未使用的依赖
	// 如果 go.sum 没有成功更新，会重试最多 maxRetries 次，每次重试递增延迟时间（1s, 2s, 4s）
	const maxRetries = 5
	const baseDelaySeconds = 1
	var lastTidyErr error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		if attempt > 1 {
			delaySeconds := baseDelaySeconds << (attempt - 2)
			emitOperationLogf("info", "等待 %d 秒后第 %d 次执行 go mod tidy (共 %d 次)", delaySeconds, attempt, maxRetries)
			time.Sleep(time.Duration(delaySeconds) * time.Second)
		} else {
			emitOperationLogf("info", "第 %d 次执行 go mod tidy (共 %d 次)", attempt, maxRetries)
		}

		if _, err := runCommand(dir, "go", "mod", "tidy"); err != nil {
			lastTidyErr = err
			emitOperationLogf("warn", "go mod tidy 执行失败：%v", err)
			continue
		}

		goSumPath := filepath.Join(dir, "go.sum")
		goSumInfo, statErr := os.Stat(goSumPath)
		if statErr != nil {
			lastTidyErr = fmt.Errorf("go.sum 文件不存在：%w", statErr)
			emitOperationLogf("warn", "go.sum 文件不存在，准备重试")
			continue
		}

		if goSumInfo.Size() == 0 {
			lastTidyErr = errors.New("go.sum 文件大小为 0，依赖同步可能不完整")
			emitOperationLogf("warn", "go.sum 文件为空，准备重试")
			continue
		}

		if err := verifyGoModDependencies(dir, versionUpdates, modulePathByRepo); err != nil {
			lastTidyErr = err
			emitOperationLogf("warn", "go.sum 验证失败：%v，准备重试", err)
			continue
		}

		emitOperationLog("info", "go mod tidy 执行成功，go.sum 已正确更新")
		lastTidyErr = nil
		break
	}

	if lastTidyErr != nil {
		return nil, fmt.Errorf("执行 go mod tidy 失败 (已重试 %d 次): %w", maxRetries, lastTidyErr)
	}

	// 按仓库名称排序更新列表，保证输出顺序稳定
	sort.Slice(updates, func(i, j int) bool {
		return updates[i].RepoName < updates[j].RepoName
	})
	return updates, nil
}

// verifyGoModDependencies 验证 go.sum 文件是否包含了所有需要的依赖版本
// 通过读取 go.sum 并检查关键依赖的校验和是否存在来确认同步成功
func verifyGoModDependencies(dir string, versionUpdates map[string]string, modulePathByRepo map[string]string) error {
	goSumPath := filepath.Join(dir, "go.sum")
	data, err := os.ReadFile(goSumPath)
	if err != nil {
		return fmt.Errorf("读取 go.sum 失败：%w", err)
	}

	content := string(data)
	for repoName, version := range versionUpdates {
		modulePath := strings.TrimSpace(modulePathByRepo[repoName])
		if modulePath == "" {
			continue
		}

		prefix := fmt.Sprintf("%s %s", modulePath, version)
		if !strings.Contains(content, prefix) {
			return fmt.Errorf("go.sum 中缺少依赖 %s@%s 的校验和", modulePath, version)
		}
	}

	return nil
}

func gitAddReleaseFiles(dir string) error {
	if _, err := runCommand(dir, "git", "add", "go.mod"); err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(dir, "go.sum")); err == nil {
		if _, err := runCommand(dir, "git", "add", "go.sum"); err != nil {
			return err
		}
	}
	return nil
}

func commitReleaseChanges(dir, repoName, tag string, updates []DependencyUpdate) error {
	parts := make([]string, 0, len(updates))
	for _, update := range updates {
		parts = append(parts, fmt.Sprintf("%s=%s", update.RepoName, update.ToVersion))
	}
	message := fmt.Sprintf("chore: release %s %s\n\nsync deps: %s", repoName, tag, strings.Join(parts, ", "))
	args := []string{"commit", "-m", message, "--only", "--", "go.mod"}
	if _, err := os.Stat(filepath.Join(dir, "go.sum")); err == nil {
		args = append(args, "go.sum")
	}
	if _, err := runCommand(dir, "git", args...); err != nil {
		return fmt.Errorf("%s 提交发布变更失败: %w", repoName, err)
	}
	return nil
}

func createGitTag(dir, tag string) error {
	if _, err := runCommand(dir, "git", "tag", tag); err != nil {
		return fmt.Errorf("创建标签 %s 失败: %w", tag, err)
	}
	return nil
}

func pushGitTag(dir, tag string) error {
	const maxRetries = 5
	var lastErr error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		if attempt > 1 {
			delaySeconds := attempt * 2
			emitOperationLogf("info", "等待 %d 秒后第 %d 次重试推送标签 %s...", delaySeconds, attempt, tag)
			time.Sleep(time.Duration(delaySeconds) * time.Second)
		}

		if _, err := runCommand(dir, "git", "push", "origin", tag); err != nil {
			lastErr = err
			emitOperationLogf("warn", "推送标签 %s 失败：%v", tag, err)
			continue
		}
		emitOperationLogf("info", "标签 %s 推送成功", tag)
		return nil
	}
	return fmt.Errorf("推送标签 %s 失败 (已重试 %d 次): %w", tag, maxRetries, lastErr)
}

func deleteGitTag(dir, tag string) error {
	if _, err := runCommand(dir, "git", "tag", "-d", tag); err != nil {
		return fmt.Errorf("删除本地标签 %s 失败: %w", tag, err)
	}
	return nil
}

func deleteRemoteGitTag(dir, tag string) error {
	refspec := fmt.Sprintf(":refs/tags/%s", tag)
	if _, err := runCommand(dir, "git", "push", "origin", refspec); err != nil {
		return fmt.Errorf("删除远端标签 %s 失败: %w", tag, err)
	}
	return nil
}

func pushBranchAndTagAtomic(dir, branch, tag string) error {
	const maxRetries = 5
	var lastErr error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		if attempt > 1 {
			delaySeconds := attempt * 2
			emitOperationLogf("info", "等待 %d 秒后第 %d 次重试推送分支 %s 与标签 %s...", delaySeconds, attempt, branch, tag)
			time.Sleep(time.Duration(delaySeconds) * time.Second)
		}
		if _, err := runCommand(dir, "git", "push", "--atomic", "origin", branch, tag); err != nil {
			lastErr = err
			emitOperationLogf("warn", "原子推送分支 %s 与标签 %s 失败：%v", branch, tag, err)
			continue
		}

		emitOperationLogf("info", "分支 %s 与标签 %s 推送成功", branch, tag)
		return nil
	}
	return fmt.Errorf("原子推送分支 %s 与标签 %s 失败 (已重试 %d 次): %w", branch, tag, maxRetries, lastErr)
}

func validateTag(tag string) error {
	if !semverTagPattern.MatchString(tag) {
		return errors.New("标签格式必须为 v主版本.次版本.补丁版本，例如 v1.2.3")
	}
	return nil
}

func nextPatchTag(latestTag string) (string, error) {
	if strings.TrimSpace(latestTag) == "" {
		return "v1.0.0", nil
	}
	matches := semverTagPattern.FindStringSubmatch(strings.TrimSpace(latestTag))
	if len(matches) == 0 {
		return "", fmt.Errorf("无法基于标签 %s 自动递增版本", latestTag)
	}
	major, _ := strconv.Atoi(matches[1])
	minor, _ := strconv.Atoi(matches[2])
	patch, _ := strconv.Atoi(matches[3])
	return fmt.Sprintf("v%d.%d.%d", major, minor, patch+1), nil
}

func validateDependencyGraph(repos []RepoConfig) error {
	graph := make(map[string][]string)
	for _, repo := range repos {
		graph[repo.Name] = append([]string(nil), repo.Dependencies...)
	}

	visiting := make(map[string]bool)
	visited := make(map[string]bool)
	var dfs func(string) error
	dfs = func(name string) error {
		if visiting[name] {
			return fmt.Errorf("检测到循环依赖: %s", name)
		}
		if visited[name] {
			return nil
		}
		visiting[name] = true
		for _, dependency := range graph[name] {
			if _, ok := graph[dependency]; !ok {
				continue
			}
			if err := dfs(dependency); err != nil {
				return err
			}
		}
		visiting[name] = false
		visited[name] = true
		return nil
	}

	for name := range graph {
		if err := dfs(name); err != nil {
			return err
		}
	}
	return nil
}

func computeDepths(root string, reverseGraph map[string][]string) map[string]int {
	depths := make(map[string]int)
	queue := []string{root}
	depths[root] = 0
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		for _, child := range reverseGraph[name] {
			nextDepth := depths[name] + 1
			depth, ok := depths[child]
			if !ok || nextDepth > depth {
				depths[child] = nextDepth
				queue = append(queue, child)
			}
		}
	}
	delete(depths, root)
	return depths
}

func probeCommand(dir, name string, args ...string) (string, error) {
	cmd := osExec.Command(name, args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func countUnpushedCommits(dir, currentBranch string) int {
	upstreamRef, ok := resolveUpstreamRef(dir, currentBranch)
	if !ok {
		return 0
	}

	output, err := probeCommand(dir, "git", "rev-list", "--count", upstreamRef+"..HEAD")
	if err != nil {
		return 0
	}

	count, err := strconv.Atoi(strings.TrimSpace(output))
	if err != nil || count < 0 {
		return 0
	}
	return count
}

func resolveUpstreamRef(dir, currentBranch string) (string, bool) {
	if strings.TrimSpace(currentBranch) == "" {
		return "", false
	}

	if output, err := probeCommand(dir, "git", "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}"); err == nil {
		ref := strings.TrimSpace(output)
		if ref != "" {
			return ref, true
		}
	}

	remoteRef := fmt.Sprintf("refs/remotes/origin/%s", currentBranch)
	if _, err := probeCommand(dir, "git", "show-ref", "--verify", remoteRef); err == nil {
		return "origin/" + currentBranch, true
	}
	return "", false
}

func runCommand(dir, name string, args ...string) (string, error) {
	return runCommandWithEnv(dir, nil, name, args...)
}

func runCommandWithEnv(dir string, env map[string]string, name string, args ...string) (string, error) {
	emitOperationLog("cmd", formatCommand(dir, name, args...))

	cmd := osExec.Command(name, args...)
	cmd.Dir = dir
	if len(env) > 0 {
		cmd.Env = os.Environ()
		for key, value := range env {
			cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", key, value))
		}
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("%s %s 创建标准输出管道失败: %w", name, strings.Join(args, " "), err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return "", fmt.Errorf("%s %s 创建错误输出管道失败: %w", name, strings.Join(args, " "), err)
	}
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("%s %s 启动失败: %w", name, strings.Join(args, " "), err)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go streamCommandOutput(stdoutPipe, &stdout, "stdout", &wg)
	go streamCommandOutput(stderrPipe, &stderr, "stderr", &wg)

	if err := cmd.Wait(); err != nil {
		wg.Wait()
		output := strings.TrimSpace(stdout.String() + "\n" + stderr.String())
		if output == "" {
			output = err.Error()
		}
		emitOperationLog("error", output)
		return "", fmt.Errorf("%s %s 执行失败: %s", name, strings.Join(args, " "), output)
	}

	wg.Wait()
	return strings.TrimSpace(stdout.String()), nil
}

func streamCommandOutput(reader io.Reader, buffer *bytes.Buffer, stream string, wg *sync.WaitGroup) {
	defer wg.Done()

	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		buffer.WriteString(line)
		buffer.WriteByte('\n')
		level := "info"
		if stream == "stderr" {
			level = "stderr"
		}
		emitOperationLog(level, line)
	}
	if err := scanner.Err(); err != nil {
		emitOperationLog("error", fmt.Sprintf("%s 输出读取失败: %v", stream, err))
	}
}

func formatCommand(dir, name string, args ...string) string {
	location := strings.TrimSpace(filepath.Base(dir))
	if location == "" || location == "." {
		location = dir
	}
	parts := append([]string{name}, args...)
	return fmt.Sprintf("[%s] %s", location, strings.Join(parts, " "))
}

func lines(text string) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	rawLines := strings.Split(text, "\n")
	result := make([]string, 0, len(rawLines))
	for _, line := range rawLines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		result = append(result, line)
	}
	return result
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
