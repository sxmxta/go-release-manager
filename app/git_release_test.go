package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCollectVersionUpdatesIncludesIndirectModules(t *testing.T) {
	repo := RepoConfig{
		Name:         "C",
		Dependencies: []string{"B"},
	}
	scan := repoScan{
		RequiredModule: map[string]string{
			"example.com/B": "v1.3.0",
			"example.com/A": "v1.9.0",
		},
	}
	changedVersions := map[string]string{
		"A": "v1.2.0",
		"B": "v1.4.0",
	}
	moduleRepoByPath := map[string]string{
		"example.com/A": "A",
		"example.com/B": "B",
	}

	updates := collectVersionUpdates(repo, scan, changedVersions, moduleRepoByPath)
	if len(updates) != 2 {
		t.Fatalf("expected 2 version updates, got %d: %#v", len(updates), updates)
	}
	if got := updates["A"]; got != "v1.2.0" {
		t.Fatalf("expected indirect module A to update to v1.2.0, got %q", got)
	}
	if got := updates["B"]; got != "v1.4.0" {
		t.Fatalf("expected direct module B to update to v1.4.0, got %q", got)
	}
}

func TestCollectVersionUpdatesSkipsUnchangedModules(t *testing.T) {
	repo := RepoConfig{
		Name:         "C",
		Dependencies: []string{"B"},
	}
	scan := repoScan{
		RequiredModule: map[string]string{
			"example.com/B": "v1.3.0",
			"example.com/A": "v1.9.0",
			"example.com/X": "v0.9.0",
		},
	}
	changedVersions := map[string]string{
		"B": "v1.4.0",
	}
	moduleRepoByPath := map[string]string{
		"example.com/A": "A",
		"example.com/B": "B",
		"example.com/X": "X",
	}

	updates := collectVersionUpdates(repo, scan, changedVersions, moduleRepoByPath)
	if len(updates) != 1 {
		t.Fatalf("expected 1 version update, got %d: %#v", len(updates), updates)
	}
	if got := updates["B"]; got != "v1.4.0" {
		t.Fatalf("expected B to update to v1.4.0, got %q", got)
	}
	if _, ok := updates["A"]; ok {
		t.Fatalf("expected A to remain unchanged, got %#v", updates)
	}
	if _, ok := updates["X"]; ok {
		t.Fatalf("expected unrelated module X to remain unchanged, got %#v", updates)
	}
}

func TestExecuteReleaseLocked_FirstReleaseSyncsDependencyChain(t *testing.T) {
	fixture := createReleaseChainFixture(t)
	store := fixture.store

	result, _, err := store.executeReleaseLocked(ReleasePayload{
		RepoName:   "engrel-one",
		Tag:        "v1.0.0",
		PushRemote: false,
	})
	if err != nil {
		t.Fatalf("executeReleaseLocked returned error: %v", err)
	}
	if result == nil {
		t.Fatalf("expected release result")
	}
	if len(result.Steps) != 3 {
		t.Fatalf("expected 3 release steps, got %d", len(result.Steps))
	}

	engrelTwoMod := readText(t, filepath.Join(fixture.repoDirs["engrel-two"], "go.mod"))
	if !strings.Contains(engrelTwoMod, "require example.com/engrel-one v1.0.0") {
		t.Fatalf("expected engrel-two go.mod to require engrel-one v1.0.0, got:\n%s", engrelTwoMod)
	}

	engrelThreeMod := readText(t, filepath.Join(fixture.repoDirs["engrel-three"], "go.mod"))
	if !strings.Contains(engrelThreeMod, "require example.com/engrel-two v1.0.0") {
		t.Fatalf("expected engrel-three go.mod to require engrel-two v1.0.0, got:\n%s", engrelThreeMod)
	}
	if !strings.Contains(engrelThreeMod, "require example.com/engrel-one v1.0.0 // indirect") {
		t.Fatalf("expected engrel-three go.mod to keep engrel-one indirect at v1.0.0, got:\n%s", engrelThreeMod)
	}

	engrelTwoTags := gitOutput(t, fixture.repoDirs["engrel-two"], "tag", "--list", "--sort=-version:refname")
	if !strings.Contains(engrelTwoTags, "v1.0.0") {
		t.Fatalf("expected engrel-two tag v1.0.0, got %q", engrelTwoTags)
	}
	engrelThreeTags := gitOutput(t, fixture.repoDirs["engrel-three"], "tag", "--list", "--sort=-version:refname")
	if !strings.Contains(engrelThreeTags, "v1.0.0") {
		t.Fatalf("expected engrel-three tag v1.0.0, got %q", engrelThreeTags)
	}
}

func TestExecuteReleaseLocked_ReusedRootTagSyncsDependencyChain(t *testing.T) {
	fixture := createReleaseChainFixture(t)
	gitRun(t, fixture.repoDirs["engrel-one"], "tag", "v1.0.0")

	store := fixture.store
	result, _, err := store.executeReleaseLocked(ReleasePayload{
		RepoName:   "engrel-one",
		Tag:        "v1.0.0",
		PushRemote: false,
	})
	if err != nil {
		t.Fatalf("executeReleaseLocked returned error: %v", err)
	}
	if result == nil || len(result.Steps) != 3 {
		t.Fatalf("expected 3 release steps, got %#v", result)
	}
	if result.Steps[0].CreatedTag {
		t.Fatalf("expected root step to reuse existing tag, got %#v", result.Steps[0])
	}

	engrelTwoMod := readText(t, filepath.Join(fixture.repoDirs["engrel-two"], "go.mod"))
	if !strings.Contains(engrelTwoMod, "require example.com/engrel-one v1.0.0") {
		t.Fatalf("expected engrel-two go.mod to require reused engrel-one v1.0.0, got:\n%s", engrelTwoMod)
	}

	engrelThreeMod := readText(t, filepath.Join(fixture.repoDirs["engrel-three"], "go.mod"))
	if !strings.Contains(engrelThreeMod, "require example.com/engrel-two v1.0.0") {
		t.Fatalf("expected engrel-three go.mod to require engrel-two v1.0.0, got:\n%s", engrelThreeMod)
	}
	if !strings.Contains(engrelThreeMod, "require example.com/engrel-one v1.0.0 // indirect") {
		t.Fatalf("expected engrel-three go.mod to keep engrel-one indirect at reused v1.0.0, got:\n%s", engrelThreeMod)
	}
}

func TestExecuteReleaseLocked_DownstreamReposIncrementOwnLatestTags(t *testing.T) {
	fixture := createReleaseChainFixture(t)
	gitRun(t, fixture.repoDirs["engrel-two"], "tag", "v1.0.4")
	gitRun(t, fixture.repoDirs["engrel-three"], "tag", "v1.0.8")

	result, _, err := fixture.store.executeReleaseLocked(ReleasePayload{
		RepoName:   "engrel-one",
		Tag:        "v1.0.7",
		PushRemote: false,
	})
	if err != nil {
		t.Fatalf("executeReleaseLocked returned error: %v", err)
	}
	if result == nil || len(result.Steps) != 3 {
		t.Fatalf("expected 3 release steps, got %#v", result)
	}

	stepsByRepo := make(map[string]ReleaseStep, len(result.Steps))
	for _, step := range result.Steps {
		stepsByRepo[step.RepoName] = step
	}

	if got := stepsByRepo["engrel-one"].NewTag; got != "v1.0.7" {
		t.Fatalf("expected engrel-one to use requested tag v1.0.7, got %q", got)
	}
	if got := stepsByRepo["engrel-two"].NewTag; got != "v1.0.5" {
		t.Fatalf("expected engrel-two to increment from v1.0.4 to v1.0.5, got %q", got)
	}
	if got := stepsByRepo["engrel-three"].NewTag; got != "v1.0.9" {
		t.Fatalf("expected engrel-three to increment from v1.0.8 to v1.0.9, got %q", got)
	}

	engrelTwoMod := readText(t, filepath.Join(fixture.repoDirs["engrel-two"], "go.mod"))
	if !strings.Contains(engrelTwoMod, "require example.com/engrel-one v1.0.7") {
		t.Fatalf("expected engrel-two go.mod to require engrel-one v1.0.7, got:\n%s", engrelTwoMod)
	}

	engrelThreeMod := readText(t, filepath.Join(fixture.repoDirs["engrel-three"], "go.mod"))
	if !strings.Contains(engrelThreeMod, "require example.com/engrel-two v1.0.5") {
		t.Fatalf("expected engrel-three go.mod to require engrel-two v1.0.5, got:\n%s", engrelThreeMod)
	}
	if !strings.Contains(engrelThreeMod, "require example.com/engrel-one v1.0.7 // indirect") {
		t.Fatalf("expected engrel-three go.mod to keep engrel-one indirect at v1.0.7, got:\n%s", engrelThreeMod)
	}

	engrelTwoTags := gitOutput(t, fixture.repoDirs["engrel-two"], "tag", "--list", "--sort=-version:refname")
	if !strings.Contains(engrelTwoTags, "v1.0.5") {
		t.Fatalf("expected engrel-two tag v1.0.5, got %q", engrelTwoTags)
	}

	engrelThreeTags := gitOutput(t, fixture.repoDirs["engrel-three"], "tag", "--list", "--sort=-version:refname")
	if !strings.Contains(engrelThreeTags, "v1.0.9") {
		t.Fatalf("expected engrel-three tag v1.0.9, got %q", engrelThreeTags)
	}
}

func TestUpdateGoModDependenciesPreservesPerModuleVersions(t *testing.T) {
	workdir := t.TempDir()
	goModPath := filepath.Join(workdir, "go.mod")
	writeText(t, goModPath, ""+
		"module example.com/engrel-three\n\n"+
		"go 1.20\n\n"+
		"require example.com/engrel-two v0.0.1\n\n"+
		"require example.com/engrel-one v0.0.1 // indirect\n")

	updates, err := updateGoModDependencies(workdir, map[string]string{
		"engrel-one": "v1.0.0",
		"engrel-two": "v0.1.0",
	}, map[string]string{
		"engrel-one": "example.com/engrel-one",
		"engrel-two": "example.com/engrel-two",
	})
	if err != nil {
		t.Fatalf("updateGoModDependencies returned error: %v", err)
	}
	if len(updates) != 2 {
		t.Fatalf("expected 2 dependency updates, got %#v", updates)
	}

	goModText := readText(t, goModPath)
	if !strings.Contains(goModText, "require example.com/engrel-two v0.1.0") {
		t.Fatalf("expected engrel-two to remain v0.1.0, got:\n%s", goModText)
	}
	if !strings.Contains(goModText, "require example.com/engrel-one v1.0.0 // indirect") {
		t.Fatalf("expected engrel-one indirect to become v1.0.0, got:\n%s", goModText)
	}
}

func TestExecuteReleaseLocked_AllowsDirtyWorktreeAndKeepsUnrelatedChanges(t *testing.T) {
	fixture := createReleaseChainFixture(t)

	dirtyFile := filepath.Join(fixture.repoDirs["engrel-two"], "local-only.txt")
	writeText(t, dirtyFile, "keep me dirty\n")

	result, _, err := fixture.store.executeReleaseLocked(ReleasePayload{
		RepoName:   "engrel-one",
		Tag:        "v1.0.0",
		PushRemote: false,
	})
	if err != nil {
		t.Fatalf("executeReleaseLocked returned error on dirty worktree: %v", err)
	}
	if result == nil || len(result.Steps) != 3 {
		t.Fatalf("expected release steps on dirty worktree, got %#v", result)
	}

	status := gitOutput(t, fixture.repoDirs["engrel-two"], "status", "--short")
	if !strings.Contains(status, "?? local-only.txt") {
		t.Fatalf("expected unrelated dirty file to remain after release, got status:\n%s", status)
	}

	lastCommit := gitOutput(t, fixture.repoDirs["engrel-two"], "show", "--stat", "--oneline", "-1")
	if strings.Contains(lastCommit, "local-only.txt") {
		t.Fatalf("expected release commit to exclude unrelated dirty file, got:\n%s", lastCommit)
	}
	if !strings.Contains(lastCommit, "go.mod") {
		t.Fatalf("expected release commit to include go.mod, got:\n%s", lastCommit)
	}
}

func TestScanRepositoryCountsUncommittedEntries(t *testing.T) {
	repoDir := filepath.Join(t.TempDir(), "scan-repo")
	writeText(t, filepath.Join(repoDir, "go.mod"), ""+
		"module example.com/scan-repo\n\n"+
		"go 1.20\n")
	writeText(t, filepath.Join(repoDir, "main.go"), ""+
		"package main\n\n"+
		"func main() {}\n")
	initGitRepo(t, repoDir)

	writeText(t, filepath.Join(repoDir, "main.go"), ""+
		"package main\n\n"+
		"func main() { println(\"dirty\") }\n")
	writeText(t, filepath.Join(repoDir, "notes.txt"), "local only\n")

	scan := scanRepository(RepoConfig{
		Name:     "scan-repo",
		LocalDir: repoDir,
	})

	if !scan.Dirty {
		t.Fatalf("expected repo scan to be dirty")
	}
	if scan.UncommittedCount != 2 {
		t.Fatalf("expected 2 uncommitted entries, got %d", scan.UncommittedCount)
	}
}

func TestScanRepositoryCountsUnpushedCommits(t *testing.T) {
	repoDir := filepath.Join(t.TempDir(), "scan-ahead-repo")
	writeText(t, filepath.Join(repoDir, "go.mod"), ""+
		"module example.com/scan-ahead-repo\n\n"+
		"go 1.20\n")
	writeText(t, filepath.Join(repoDir, "main.go"), ""+
		"package main\n\n"+
		"func main() {}\n")
	initGitRepo(t, repoDir)

	initialCommit := gitOutput(t, repoDir, "rev-parse", "HEAD")
	gitRun(t, repoDir, "remote", "add", "origin", "https://example.com/mock.git")
	gitRun(t, repoDir, "update-ref", "refs/remotes/origin/main", initialCommit)

	writeText(t, filepath.Join(repoDir, "main.go"), ""+
		"package main\n\n"+
		"func main() { println(\"ahead\") }\n")
	gitRun(t, repoDir, "add", "main.go")
	gitRun(t, repoDir, "commit", "-m", "ahead commit")
	gitRun(t, repoDir, "branch", "--set-upstream-to=origin/main", "main")

	scan := scanRepository(RepoConfig{
		Name:     "scan-ahead-repo",
		LocalDir: repoDir,
	})

	if scan.UnpushedCommitCount != 1 {
		t.Fatalf("expected 1 unpushed commit, got %d", scan.UnpushedCommitCount)
	}
}

func TestGoModTidyLocked_CompletesForCurrentRepo(t *testing.T) {
	fixture := createSingleRepoFixture(t, "tidy-repo", ""+
		"module example.com/tidy-repo\n\n"+
		"go 1.20\n", ""+
		"package main\n\n"+
		"func main() {}\n")

	message, err := fixture.store.goModTidyLocked(RepoActionPayload{RepoName: "tidy-repo"})
	if err != nil {
		t.Fatalf("goModTidyLocked returned error: %v", err)
	}
	if !strings.Contains(message, "tidy-repo 已完成 go mod tidy") {
		t.Fatalf("expected tidy summary, got %q", message)
	}
}

func TestGitCommitLocked_StagesAndCommitsAllChanges(t *testing.T) {
	fixture := createSingleRepoFixture(t, "git-ops", ""+
		"module example.com/git-ops\n\n"+
		"go 1.20\n", ""+
		"package main\n\n"+
		"func main() {}\n")

	writeText(t, filepath.Join(fixture.repoDir, "notes.txt"), "hello commit\n")

	message, err := fixture.store.gitCommitLocked(GitCommitPayload{
		RepoName: "git-ops",
		Message:  "chore: save notes",
	})
	if err != nil {
		t.Fatalf("gitCommitLocked returned error: %v", err)
	}
	if !strings.Contains(message, "git-ops 已提交") {
		t.Fatalf("expected commit summary, got %q", message)
	}

	lastSubject := gitOutput(t, fixture.repoDir, "log", "-1", "--pretty=%s")
	if lastSubject != "chore: save notes" {
		t.Fatalf("expected last commit subject to match, got %q", lastSubject)
	}

	lastCommit := gitOutput(t, fixture.repoDir, "show", "--stat", "--oneline", "-1")
	if !strings.Contains(lastCommit, "notes.txt") {
		t.Fatalf("expected new file to be included in commit, got:\n%s", lastCommit)
	}
}

func TestGitPushLocked_PushesCurrentBranchToOrigin(t *testing.T) {
	fixture := createSingleRepoFixture(t, "push-repo", ""+
		"module example.com/push-repo\n\n"+
		"go 1.20\n", ""+
		"package main\n\n"+
		"func main() {}\n")

	remoteDir := filepath.Join(t.TempDir(), "push-repo-remote.git")
	if err := os.MkdirAll(remoteDir, 0755); err != nil {
		t.Fatalf("mkdir remote failed: %v", err)
	}
	gitRun(t, remoteDir, "init", "--bare")
	gitRun(t, fixture.repoDir, "remote", "add", "origin", remoteDir)

	message, err := fixture.store.gitPushLocked(RepoActionPayload{RepoName: "push-repo"})
	if err != nil {
		if strings.Contains(err.Error(), "couldn't create signal pipe") {
			t.Skipf("skipping push verification in current git environment: %v", err)
		}
		t.Fatalf("gitPushLocked returned error: %v", err)
	}
	if !strings.Contains(message, "push-repo 已推送当前分支 main 到 origin") {
		t.Fatalf("expected push summary, got %q", message)
	}

	remoteHead := gitOutput(t, fixture.repoDir, "ls-remote", "--heads", "origin", "main")
	if strings.TrimSpace(remoteHead) == "" {
		t.Fatalf("expected origin/main to exist after push")
	}
}

func TestPushUnpushedTagsLocked_PushesOnlyMissingTags(t *testing.T) {
	fixture := createSingleRepoFixture(t, "tag-repo", ""+
		"module example.com/tag-repo\n\n"+
		"go 1.20\n", ""+
		"package main\n\n"+
		"func main() {}\n")

	remoteDir := filepath.Join(t.TempDir(), "tag-repo-remote.git")
	if err := os.MkdirAll(remoteDir, 0755); err != nil {
		t.Fatalf("mkdir remote failed: %v", err)
	}
	gitRun(t, remoteDir, "init", "--bare")
	gitRun(t, fixture.repoDir, "remote", "add", "origin", remoteDir)
	gitRun(t, fixture.repoDir, "tag", "v1.0.0")
	gitRun(t, fixture.repoDir, "tag", "v1.0.1")

	message, err := fixture.store.pushUnpushedTagsLocked(RepoActionPayload{RepoName: "tag-repo"})
	if err != nil {
		if strings.Contains(err.Error(), "couldn't create signal pipe") {
			t.Skipf("skipping tag push verification in current git environment: %v", err)
		}
		t.Fatalf("pushUnpushedTagsLocked returned error: %v", err)
	}
	if !strings.Contains(message, "tag-repo 已推送 2 个本地标签到 origin") {
		t.Fatalf("expected push tags summary, got %q", message)
	}

	remoteTags := gitOutput(t, fixture.repoDir, "ls-remote", "--tags", "--refs", "origin")
	if !strings.Contains(remoteTags, "refs/tags/v1.0.0") || !strings.Contains(remoteTags, "refs/tags/v1.0.1") {
		t.Fatalf("expected both tags on origin, got:\n%s", remoteTags)
	}
}

type singleRepoFixture struct {
	store   *releaseStore
	repoDir string
}

func createSingleRepoFixture(t *testing.T, repoName, goModContent, sourceContent string) singleRepoFixture {
	t.Helper()

	repoDir := filepath.Join(t.TempDir(), repoName)
	writeText(t, filepath.Join(repoDir, "go.mod"), goModContent)
	writeText(t, filepath.Join(repoDir, "main.go"), sourceContent)
	initGitRepo(t, repoDir)

	store := &releaseStore{
		loaded: true,
		config: AppConfig{
			Repos: []RepoConfig{
				{
					Name:               repoName,
					LocalDir:           repoDir,
					ModulePath:         "example.com/" + repoName,
					ReleaseBranch:      "main",
					Dependencies:       []string{},
					DependenciesManual: true,
				},
			},
			SelectedRepo: repoName,
		},
	}

	return singleRepoFixture{
		store:   store,
		repoDir: repoDir,
	}
}

type releaseChainFixture struct {
	store    *releaseStore
	repoDirs map[string]string
}

func createReleaseChainFixture(t *testing.T) releaseChainFixture {
	t.Helper()

	workdir := t.TempDir()
	previousWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}
	if err := os.Chdir(workdir); err != nil {
		t.Fatalf("chdir failed: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(previousWD)
	})

	repoDirs := map[string]string{
		"engrel-one":   filepath.Join(workdir, "engrel-one"),
		"engrel-two":   filepath.Join(workdir, "engrel-two"),
		"engrel-three": filepath.Join(workdir, "engrel-three"),
	}

	writeText(t, filepath.Join(repoDirs["engrel-one"], "go.mod"), ""+
		"module example.com/engrel-one\n\n"+
		"go 1.20\n")
	writeText(t, filepath.Join(repoDirs["engrel-one"], "engrelone.go"), ""+
		"package engrelone\n\n"+
		"func Version() string { return \"engrel-one\" }\n")

	writeText(t, filepath.Join(repoDirs["engrel-two"], "go.mod"), ""+
		"module example.com/engrel-two\n\n"+
		"go 1.20\n\n"+
		"require example.com/engrel-one v0.0.1\n\n"+
		"replace example.com/engrel-one => ../engrel-one\n")
	writeText(t, filepath.Join(repoDirs["engrel-two"], "engreltwo.go"), ""+
		"package engreltwo\n\n"+
		"import one \"example.com/engrel-one\"\n\n"+
		"func Version() string { return one.Version() + \"/engrel-two\" }\n")

	writeText(t, filepath.Join(repoDirs["engrel-three"], "go.mod"), ""+
		"module example.com/engrel-three\n\n"+
		"go 1.20\n\n"+
		"require example.com/engrel-two v0.0.1\n\n"+
		"require example.com/engrel-one v0.0.1 // indirect\n\n"+
		"replace example.com/engrel-two => ../engrel-two\n"+
		"replace example.com/engrel-one => ../engrel-one\n")
	writeText(t, filepath.Join(repoDirs["engrel-three"], "engrelthree.go"), ""+
		"package engrelthree\n\n"+
		"import two \"example.com/engrel-two\"\n\n"+
		"func Version() string { return two.Version() + \"/engrel-three\" }\n")

	for _, repoDir := range repoDirs {
		initGitRepo(t, repoDir)
	}

	store := &releaseStore{
		loaded: true,
		config: AppConfig{
			Repos: []RepoConfig{
				{
					Name:               "engrel-one",
					LocalDir:           repoDirs["engrel-one"],
					ModulePath:         "example.com/engrel-one",
					ReleaseBranch:      "main",
					Dependencies:       []string{},
					DependenciesManual: true,
				},
				{
					Name:               "engrel-two",
					LocalDir:           repoDirs["engrel-two"],
					ModulePath:         "example.com/engrel-two",
					ReleaseBranch:      "main",
					Dependencies:       []string{"engrel-one"},
					DependenciesManual: true,
				},
				{
					Name:               "engrel-three",
					LocalDir:           repoDirs["engrel-three"],
					ModulePath:         "example.com/engrel-three",
					ReleaseBranch:      "main",
					Dependencies:       []string{"engrel-two"},
					DependenciesManual: true,
				},
			},
			SelectedRepo: "engrel-one",
		},
	}

	return releaseChainFixture{
		store:    store,
		repoDirs: repoDirs,
	}
}

func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	gitRun(t, dir, "init")
	gitRun(t, dir, "config", "user.email", "test@example.com")
	gitRun(t, dir, "config", "user.name", "Codex Test")
	gitRun(t, dir, "branch", "-M", "main")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "init")
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed in %s: %v\n%s", strings.Join(args, " "), dir, err, string(output))
	}
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed in %s: %v\n%s", strings.Join(args, " "), dir, err, string(output))
	}
	return strings.TrimSpace(string(output))
}

func writeText(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir failed for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write failed for %s: %v", path, err)
	}
}

func readText(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read failed for %s: %v", path, err)
	}
	return string(data)
}
