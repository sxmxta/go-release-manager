package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func (m *releaseStore) goModTidyLocked(payload RepoActionPayload) (string, error) {
	repo, scan, err := m.repoForActionLocked(payload.RepoName, repoScanOptions{
		includeGoMod: true,
	})
	if err != nil {
		return "", err
	}
	if !scan.HasGoMod {
		return "", fmt.Errorf("%s 缺少 go.mod，无法执行 go mod tidy", repo.Name)
	}

	emitOperationLogf("info", "开始执行 go mod tidy: %s", repo.Name)
	env, cleanup, err := createGoCommandEnv(repo.LocalDir)
	if err != nil {
		return "", fmt.Errorf("%s 准备 go mod tidy 环境失败: %w", repo.Name, err)
	}
	defer cleanup()

	if _, err := runCommandWithEnv(repo.LocalDir, env, "go", "mod", "tidy"); err != nil {
		return "", fmt.Errorf("%s 执行 go mod tidy 失败: %w", repo.Name, err)
	}

	changedFiles, err := gitStatusFiles(repo.LocalDir, "go.mod", "go.sum")
	if err != nil {
		return "", fmt.Errorf("%s 读取 tidy 结果失败: %w", repo.Name, err)
	}

	message := fmt.Sprintf("%s 已完成 go mod tidy", repo.Name)
	if len(changedFiles) == 0 {
		message += "，未检测到 go.mod 或 go.sum 变更"
	} else {
		message += fmt.Sprintf("，当前更新文件: %s", strings.Join(changedFiles, ", "))
	}
	emitOperationLog("info", message)
	return message, nil
}

func (m *releaseStore) gitCommitLocked(payload GitCommitPayload) (string, error) {
	repo, scan, err := m.repoForActionLocked(payload.RepoName, repoScanOptions{
		includeStatus: true,
	})
	if err != nil {
		return "", err
	}
	message := strings.TrimSpace(payload.Message)
	if message == "" {
		return "", errors.New("提交信息不能为空")
	}
	if !scan.Dirty {
		return "", fmt.Errorf("%s 当前没有可提交的改动", repo.Name)
	}

	emitOperationLogf("info", "开始提交仓库: %s", repo.Name)
	if _, err := runCommand(repo.LocalDir, "git", "add", "-A"); err != nil {
		return "", fmt.Errorf("%s 暂存改动失败: %w", repo.Name, err)
	}

	stagedFilesOutput, err := runCommand(repo.LocalDir, "git", "diff", "--cached", "--name-only")
	if err != nil {
		return "", fmt.Errorf("%s 读取暂存文件失败: %w", repo.Name, err)
	}
	stagedFiles := lines(stagedFilesOutput)
	if len(stagedFiles) == 0 {
		return "", fmt.Errorf("%s 暂未检测到可提交的改动", repo.Name)
	}

	if _, err := runCommand(repo.LocalDir, "git", "commit", "-m", message); err != nil {
		return "", fmt.Errorf("%s 提交失败: %w", repo.Name, err)
	}

	commitID, err := runCommand(repo.LocalDir, "git", "rev-parse", "--short", "HEAD")
	if err != nil {
		return "", fmt.Errorf("%s 读取提交结果失败: %w", repo.Name, err)
	}

	branch := strings.TrimSpace(scan.CurrentBranch)
	if branch == "" {
		branch, _ = runCommand(repo.LocalDir, "git", "branch", "--show-current")
		branch = strings.TrimSpace(branch)
	}

	summary := fmt.Sprintf("%s 已提交 %s", repo.Name, strings.TrimSpace(commitID))
	if branch != "" {
		summary += fmt.Sprintf("，当前分支 %s", branch)
	}
	summary += fmt.Sprintf("，文件: %s", strings.Join(stagedFiles, ", "))
	emitOperationLog("info", summary)
	return summary, nil
}

func (m *releaseStore) gitPushLocked(payload RepoActionPayload) (string, error) {
	repo, scan, err := m.repoForActionLocked(payload.RepoName, repoScanOptions{
		includeStatus: true,
	})
	if err != nil {
		return "", err
	}

	branch := strings.TrimSpace(scan.CurrentBranch)
	if branch == "" {
		return "", fmt.Errorf("%s 当前没有可推送的分支", repo.Name)
	}

	emitOperationLogf("info", "开始推送仓库 %s 的当前分支 %s 到 origin", repo.Name, branch)
	if _, err := runCommand(repo.LocalDir, "git", "push", "origin", branch); err != nil {
		return "", fmt.Errorf("%s 推送分支 %s 到 origin 失败: %w", repo.Name, branch, err)
	}

	message := fmt.Sprintf("%s 已推送当前分支 %s 到 origin", repo.Name, branch)
	emitOperationLog("info", message)
	return message, nil
}

func (m *releaseStore) pushUnpushedTagsLocked(payload RepoActionPayload) (string, error) {
	repo, scan, err := m.repoForActionLocked(payload.RepoName, repoScanOptions{
		includeTags: true,
	})
	if err != nil {
		return "", err
	}
	if len(scan.Tags) == 0 {
		return "", fmt.Errorf("%s 当前没有可推送的本地标签", repo.Name)
	}

	remoteTags, err := listRemoteTags(repo.LocalDir)
	if err != nil {
		return "", fmt.Errorf("%s 读取 origin 远端标签失败: %w", repo.Name, err)
	}
	pendingTags := diffUnpushedTags(scan.Tags, remoteTags)
	if len(pendingTags) == 0 {
		message := fmt.Sprintf("%s 没有需要推送的本地标签", repo.Name)
		emitOperationLog("info", message)
		return message, nil
	}

	emitOperationLogf("info", "开始推送 %s 的未推送本地标签: %s", repo.Name, strings.Join(pendingTags, ", "))
	args := append([]string{"push", "origin"}, pendingTags...)
	if _, err := runCommand(repo.LocalDir, "git", args...); err != nil {
		return "", fmt.Errorf("%s 推送未推送本地标签失败: %w", repo.Name, err)
	}

	message := fmt.Sprintf("%s 已推送 %d 个本地标签到 origin: %s", repo.Name, len(pendingTags), strings.Join(pendingTags, ", "))
	emitOperationLog("info", message)
	return message, nil
}

func (m *releaseStore) repoForActionLocked(repoName string, options repoScanOptions) (RepoConfig, repoScan, error) {
	name := strings.TrimSpace(repoName)
	if name == "" {
		return RepoConfig{}, repoScan{}, errors.New("请先选择仓库")
	}

	for _, repo := range m.config.Repos {
		if repo.Name != name {
			continue
		}
		scan := scanRepositoryWithOptions(repo, options)
		if !scan.Exists {
			return RepoConfig{}, repoScan{}, fmt.Errorf("%s 的本地目录不存在", name)
		}
		if !scan.IsGitRepo {
			return RepoConfig{}, repoScan{}, fmt.Errorf("%s 当前目录不是可操作的 git 仓库", name)
		}
		return repo, scan, nil
	}

	return RepoConfig{}, repoScan{}, fmt.Errorf("仓库 %s 不存在", name)
}

func createGoCommandEnv(baseDir string) (map[string]string, func(), error) {
	root, err := os.MkdirTemp(baseDir, ".energy-release-go-")
	if err != nil {
		return nil, nil, err
	}

	gocache := filepath.Join(root, "gocache")
	gotmp := filepath.Join(root, "gotmp")
	if err := os.MkdirAll(gocache, 0755); err != nil {
		_ = os.RemoveAll(root)
		return nil, nil, err
	}
	if err := os.MkdirAll(gotmp, 0755); err != nil {
		_ = os.RemoveAll(root)
		return nil, nil, err
	}

	cleanup := func() {
		_ = os.RemoveAll(root)
	}
	return map[string]string{
		"GOCACHE":  gocache,
		"GOTMPDIR": gotmp,
	}, cleanup, nil
}

func gitStatusFiles(dir string, files ...string) ([]string, error) {
	args := []string{"status", "--short", "--"}
	args = append(args, files...)
	output, err := runCommand(dir, "git", args...)
	if err != nil {
		return nil, err
	}

	result := make([]string, 0, len(files))
	for _, line := range lines(output) {
		if len(line) < 4 {
			continue
		}
		name := strings.TrimSpace(line[3:])
		if name == "" {
			continue
		}
		result = append(result, name)
	}
	return uniqueStrings(result), nil
}

func listRemoteTags(dir string) (map[string]struct{}, error) {
	output, err := runCommand(dir, "git", "ls-remote", "--tags", "--refs", "origin")
	if err != nil {
		return nil, err
	}

	result := make(map[string]struct{})
	for _, line := range lines(output) {
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		ref := strings.TrimSpace(parts[len(parts)-1])
		if !strings.HasPrefix(ref, "refs/tags/") {
			continue
		}
		tag := strings.TrimPrefix(ref, "refs/tags/")
		if tag == "" {
			continue
		}
		result[tag] = struct{}{}
	}
	return result, nil
}

func diffUnpushedTags(localTags []string, remoteTags map[string]struct{}) []string {
	result := make([]string, 0, len(localTags))
	for _, tag := range localTags {
		if _, ok := remoteTags[tag]; ok {
			continue
		}
		result = append(result, tag)
	}
	sort.Strings(result)
	return result
}
