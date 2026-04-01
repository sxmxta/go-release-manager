package app

type AppConfig struct {
	Repos        []RepoConfig `json:"repos"`
	SelectedRepo string       `json:"selectedRepo"`
}

type RepoConfig struct {
	Name               string   `json:"name"`
	RemoteURL          string   `json:"remoteUrl"`
	LocalDir           string   `json:"localDir"`
	ModulePath         string   `json:"modulePath"`
	ReleaseBranch      string   `json:"releaseBranch"`
	Dependencies       []string `json:"dependencies"`
	DependenciesManual bool     `json:"dependenciesManual,omitempty"`
}

type LogEntry struct {
	Time    string `json:"time"`
	Level   string `json:"level"`
	Message string `json:"message"`
}

type AppState struct {
	Repos        []RepoState `json:"repos"`
	SelectedRepo string      `json:"selectedRepo"`
	Logs         []LogEntry  `json:"logs"`
}

type RepoState struct {
	Name                string   `json:"name"`
	RemoteURL           string   `json:"remoteUrl"`
	LocalDir            string   `json:"localDir"`
	ModulePath          string   `json:"modulePath"`
	ReleaseBranch       string   `json:"releaseBranch"`
	Dependencies        []string `json:"dependencies"`
	Downstreams         []string `json:"downstreams"`
	CascadeTargets      []string `json:"cascadeTargets"`
	Branches            []string `json:"branches"`
	CurrentBranch       string   `json:"currentBranch"`
	Tags                []string `json:"tags"`
	LatestTag           string   `json:"latestTag"`
	Dirty               bool     `json:"dirty"`
	UncommittedCount    int      `json:"uncommittedCount"`
	UnpushedCommitCount int      `json:"unpushedCommitCount"`
	Exists              bool     `json:"exists"`
	IsGitRepo           bool     `json:"isGitRepo"`
	HasGoMod            bool     `json:"hasGoMod"`
	LastScanError       string   `json:"lastScanError"`
	ReleaseReady        bool     `json:"releaseReady"`
	DependenciesLabel   string   `json:"dependenciesLabel"`
}

type RepoPayload struct {
	Name               string   `json:"name"`
	RemoteURL          string   `json:"remoteUrl"`
	LocalDir           string   `json:"localDir"`
	ModulePath         string   `json:"modulePath"`
	ReleaseBranch      string   `json:"releaseBranch"`
	Dependencies       []string `json:"dependencies"`
	DependenciesManual bool     `json:"dependenciesManual"`
}

type DeleteRepoPayload struct {
	Name string `json:"name"`
}

type DeleteTagPayload struct {
	RepoName     string `json:"repoName"`
	Tag          string `json:"tag"`
	DeleteRemote bool   `json:"deleteRemote"`
}

type RepoActionPayload struct {
	RepoName string `json:"repoName"`
}

type GitCommitPayload struct {
	RepoName string `json:"repoName"`
	Message  string `json:"message"`
}

type SelectRepoPayload struct {
	Name string `json:"name"`
}

type ReleasePayload struct {
	RepoName   string `json:"repoName"`
	Tag        string `json:"tag"`
	PushRemote bool   `json:"pushRemote"`
}

type ActionResponse struct {
	OK      bool           `json:"ok"`
	Message string         `json:"message"`
	State   *AppState      `json:"state,omitempty"`
	Release *ReleaseResult `json:"release,omitempty"`
}

type OperationEvent struct {
	Action  string         `json:"action"`
	OK      bool           `json:"ok"`
	Message string         `json:"message"`
	Release *ReleaseResult `json:"release,omitempty"`
}

type ReleaseResult struct {
	RootRepo string        `json:"rootRepo"`
	RootTag  string        `json:"rootTag"`
	Steps    []ReleaseStep `json:"steps"`
}

type ReleaseStep struct {
	RepoName            string             `json:"repoName"`
	Branch              string             `json:"branch"`
	PreviousTag         string             `json:"previousTag"`
	NewTag              string             `json:"newTag"`
	UpdatedDependencies []DependencyUpdate `json:"updatedDependencies"`
	CreatedCommit       bool               `json:"createdCommit"`
	CreatedTag          bool               `json:"createdTag"`
	Pushed              bool               `json:"pushed"`
	Message             string             `json:"message"`
}

type DependencyUpdate struct {
	RepoName    string `json:"repoName"`
	ModulePath  string `json:"modulePath"`
	FromVersion string `json:"fromVersion"`
	ToVersion   string `json:"toVersion"`
}

type repoScan struct {
	Branches            []string
	CurrentBranch       string
	Tags                []string
	LatestTag           string
	ModulePath          string
	RequiredModule      map[string]string
	Dirty               bool
	UncommittedCount    int
	UnpushedCommitCount int
	Exists              bool
	IsGitRepo           bool
	HasGoMod            bool
	LastScanError       string
}
