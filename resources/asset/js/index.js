window.onload = () => {
    const $ = (id) => document.getElementById(id);
    const MAX_LOG_ENTRIES = 500;

    const elements = {
        min: $("min"),
        close: $("close"),
        repoList: $("repoList"),
        showAddRepo: $("showAddRepo"),
        refreshRepos: $("refreshRepos"),
        addRepoModal: $("addRepoModal"),
        newRepoName: $("newRepoName"),
        confirmAddRepo: $("confirmAddRepo"),
        closeAddRepoModal: $("closeAddRepoModal"),
        gitActionModal: $("gitActionModal"),
        gitCommitMessage: $("gitCommitMessage"),
        confirmGitCommit: $("confirmGitCommit"),
        confirmGitPush: $("confirmGitPush"),
        closeGitActionModal: $("closeGitActionModal"),
        confirmActionModal: $("confirmActionModal"),
        confirmActionTitle: $("confirmActionTitle"),
        confirmActionBody: $("confirmActionBody"),
        cancelConfirmAction: $("cancelConfirmAction"),
        confirmActionButton: $("confirmActionButton"),
        busyMask: $("busyMask"),
        busyText: $("busyText"),
        busyMeta: $("busyMeta"),
        busyStep: $("busyStep"),
        repoName: $("repoName"),
        modulePath: $("modulePath"),
        repoUrl: $("repoUrl"),
        localDir: $("localDir"),
        releaseBranch: $("releaseBranch"),
        dependencyPicker: $("dependencyPicker"),
        scanStatus: $("scanStatus"),
        currentBranch: $("currentBranch"),
        latestTag: $("latestTag"),
        downstreamCount: $("downstreamCount"),
        dirtyState: $("dirtyState"),
        saveCurrentRepo: $("saveCurrentRepo"),
        newTag: $("newTag"),
        pushRemote: $("pushRemote"),
        createRelease: $("createRelease"),
        pushUnpushedTags: $("pushUnpushedTags"),
        impactCount: $("impactCount"),
        impactList: $("impactList"),
        deleteTagName: $("deleteTagName"),
        deleteRemoteTag: $("deleteRemoteTag"),
        deleteTag: $("deleteTag"),
        runGoModTidy: $("runGoModTidy"),
        showGitActionModal: $("showGitActionModal"),
        releaseSummary: $("releaseSummary"),
        log: $("log")
    };

    let appState = {
        repos: [],
        selectedRepo: "",
        logs: []
    };
    let lastRelease = null;
    let lastMessage = "正在初始化页面";
    let lastSelectedRepo = "";
    let lastDeleteTag = "";
    let isBusy = false;
    let busyDescriptor = createIdleBusyDescriptor();
    let pendingConfirmAction = null;

    function applyState(nextState) {
        appState = {
            repos: Array.isArray(nextState?.repos) ? nextState.repos : [],
            selectedRepo: nextState?.selectedRepo || "",
            logs: Array.isArray(nextState?.logs) ? nextState.logs.slice(-MAX_LOG_ENTRIES) : []
        };
        if (!appState.selectedRepo && appState.repos.length > 0) {
            appState.selectedRepo = appState.repos[0].name;
        }
        render();
    }

    function appendLogEntry(entry) {
        if (!entry || !entry.message) {
            return;
        }
        appState.logs = [...appState.logs, entry].slice(-MAX_LOG_ENTRIES);
        updateBusyStepFromLog(entry);
        renderLogs();
    }

    function currentRepo() {
        return appState.repos.find((repo) => repo.name === appState.selectedRepo) || null;
    }

    function createIdleBusyDescriptor() {
        return {
            text: "正在处理，请稍候...",
            meta: "关键操作：等待任务开始",
            step: "当前步骤：等待后台实时日志"
        };
    }

    function buildBusyDescriptor(name, payload, fallbackText) {
        const repoName = payload?.repoName || payload?.name || currentRepo()?.name || appState.selectedRepo || "当前仓库";
        const tagName = payload?.tag || elements.newTag?.value?.trim() || "-";
        const localDir = typeof payload?.localDir === "string" ? payload.localDir.trim() : "";
        const activeRepo = currentRepo();
        const reusingHistoricalTag = name === "create-release"
            && !!tagName
            && Array.isArray(activeRepo?.tags)
            && activeRepo.tags.includes(tagName);
        const releaseBranch = typeof payload?.releaseBranch === "string" && payload.releaseBranch.trim()
            ? payload.releaseBranch.trim()
            : "跟随当前分支";

        switch (name) {
            case "page-ready":
                return {
                    text: fallbackText || "正在加载项目状态...",
                    meta: "关键操作：读取配置并初始化仓库工作区",
                    step: "当前步骤：等待后台加载仓库、分支、标签和依赖"
                };
            case "save-repo":
                return {
                    text: fallbackText || "正在保存仓库配置...",
                    meta: `关键操作：保存仓库 ${repoName}${localDir ? ` · ${localDir}` : ""}`,
                    step: `当前步骤：准备写入 config.json，并重新扫描发布分支 ${releaseBranch}`
                };
            case "create-release":
                return {
                    text: fallbackText || (reusingHistoricalTag ? "正在复用历史标签并联动发布..." : "正在执行级联发布..."),
                    meta: reusingHistoricalTag
                        ? `关键操作：复用 ${repoName} 的历史标签 ${tagName}${payload?.pushRemote ? " · 同步检查并推送 origin" : ""}`
                        : `关键操作：为 ${repoName} 创建标签 ${tagName}${payload?.pushRemote ? " · 同步推送 origin" : ""}`,
                    step: reusingHistoricalTag
                        ? "当前步骤：根仓库跳过重复打标签，下游仓库继续按依赖层级升级"
                        : "当前步骤：将按依赖层级更新 go.mod、提交并创建标签"
                };
            case "push-unpushed-tags":
                return {
                    text: fallbackText || "正在推送未推送本地标签...",
                    meta: `关键操作：推送 ${repoName} 未同步到 origin 的本地标签`,
                    step: "当前步骤：先比对本地和 origin 标签差异，再逐批推送缺失标签"
                };
            case "delete-tag":
                return {
                    text: fallbackText || "正在删除标签...",
                    meta: `关键操作：删除 ${repoName} 的标签 ${tagName}${payload?.deleteRemote ? " · 包含 origin" : ""}`,
                    step: "当前步骤：先删除本地标签，再按选项处理远端标签"
                };
            case "go-mod-tidy":
                return {
                    text: fallbackText || "正在执行 go mod tidy...",
                    meta: `关键操作：刷新 ${repoName} 的 Go 模块依赖`,
                    step: "当前步骤：等待 go mod tidy 输出依赖整理结果"
                };
            case "git-commit":
                return {
                    text: fallbackText || "正在提交当前改动...",
                    meta: `关键操作：提交 ${repoName} 的当前工作区`,
                    step: "当前步骤：先暂存全部改动，再生成新的 Git 提交"
                };
            case "git-push":
                return {
                    text: fallbackText || "正在推送当前分支...",
                    meta: `关键操作：推送 ${repoName} 当前分支到 origin`,
                    step: "当前步骤：等待 git push 返回远端结果"
                };
            case "refresh-repos":
                return {
                    text: fallbackText || "正在扫描仓库...",
                    meta: "关键操作：刷新全部仓库的目录、分支、标签和依赖关系",
                    step: "当前步骤：等待后台逐个仓库输出扫描日志"
                };
            case "select-repo":
                return {
                    text: fallbackText || "正在切换仓库...",
                    meta: `关键操作：切换到 ${repoName}`,
                    step: "当前步骤：加载该仓库的工作区状态和标签列表"
                };
            case "delete-repo":
                return {
                    text: fallbackText || "正在删除仓库...",
                    meta: `关键操作：删除仓库 ${repoName}`,
                    step: "当前步骤：移除配置，并重新整理依赖关系"
                };
            default:
                return {
                    text: fallbackText || "正在处理，请稍候...",
                    meta: `关键操作：${repoName}`,
                    step: "当前步骤：等待后台返回执行进度"
                };
        }
    }

    function applyBusyDescriptor(descriptor) {
        busyDescriptor = {
            ...createIdleBusyDescriptor(),
            ...(descriptor || {})
        };
        elements.busyText.textContent = busyDescriptor.text;
        elements.busyMeta.textContent = busyDescriptor.meta;
        elements.busyStep.textContent = busyDescriptor.step;
    }

    function updateBusyStepFromLog(entry) {
        if (!isBusy || !entry?.message) {
            return;
        }
        const message = String(entry.message).trim();
        if (!message) {
            return;
        }
        const level = String(entry.level || "info").toUpperCase();
        busyDescriptor.step = `当前步骤 [${level}]：${message}`;
        elements.busyStep.textContent = busyDescriptor.step;
    }

    function setBusy(active, descriptor) {
        isBusy = active;
        if (active) {
            applyBusyDescriptor(descriptor);
        } else {
            applyBusyDescriptor(createIdleBusyDescriptor());
        }
        elements.busyMask.style.display = active ? "flex" : "none";
        render();
    }

    function startAsyncAction(name, payload, busyText, onAccepted) {
        if (isBusy) {
            return;
        }

        setBusy(true, buildBusyDescriptor(name, payload, busyText));

        const ackHandler = (response) => {
            if (response?.ok === false) {
                if (response.state) {
                    applyState(response.state);
                }
                if (response.release) {
                    lastRelease = response.release;
                } else if (name === "create-release") {
                    lastRelease = null;
                }
                if (response.message) {
                    lastMessage = response.message;
                }
                setBusy(false);
                renderReleaseSummary(response);
                return;
            }
            if (typeof onAccepted === "function") {
                onAccepted(response || {});
            }
        };

        if (typeof payload === "undefined") {
            ipc.emit(name, ackHandler);
            return;
        }
        ipc.emit(name, [JSON.stringify(payload)], ackHandler);
    }

    function render() {
        renderRepoList();
        renderConfig();
        renderImpact();
        renderTags();
        renderLogs();
        renderReleaseSummary();
    }

    function applyStateTone(element, tone) {
        if (!element) {
            return;
        }
        element.classList.remove("state-good", "state-bad", "state-neutral");
        element.classList.add(tone || "state-neutral");
    }

    function syncWorkspaceStateTone(repo) {
        if (!repo) {
            applyStateTone(elements.scanStatus, "state-neutral");
            applyStateTone(elements.currentBranch, "state-neutral");
            applyStateTone(elements.latestTag, "state-neutral");
            applyStateTone(elements.downstreamCount, "state-neutral");
            applyStateTone(elements.dirtyState, "state-neutral");
            return;
        }

        const downstreamCount = Array.isArray(repo.cascadeTargets) ? repo.cascadeTargets.length : 0;
        const scanTone = repo.lastScanError || !repo.exists || !repo.isGitRepo || repo.dirty ? "state-bad" : "state-good";

        applyStateTone(elements.scanStatus, scanTone);
        applyStateTone(elements.currentBranch, repo.currentBranch ? "state-good" : "state-bad");
        applyStateTone(elements.latestTag, repo.latestTag ? "state-good" : "state-bad");
        applyStateTone(elements.downstreamCount, downstreamCount > 0 ? "state-good" : "state-neutral");
        applyStateTone(elements.dirtyState, repo.dirty ? "state-bad" : "state-good");
    }

    function renderRepoList() {
        if (appState.repos.length === 0) {
            elements.repoList.innerHTML = `<div class="empty-state">暂无仓库，请先新增。</div>`;
            return;
        }

        elements.repoList.innerHTML = appState.repos.map((repo) => {
            const selected = repo.name === appState.selectedRepo ? "active" : "";
            const statusClass = repo.lastScanError ? "warn" : repo.dirty ? "busy" : "ok";
            const statusText = repo.lastScanError ? "待处理" : repo.dirty ? "有改动" : "就绪";
            const unpushedCommitCount = Number.isFinite(repo.unpushedCommitCount) ? repo.unpushedCommitCount : 0;
            const pendingClass = unpushedCommitCount > 0 ? "active" : "idle";
            const copy = repo.modulePath || repo.localDir || "未配置模块路径和本地目录";
            const currentBranchDisplay = repo.currentBranch || "-";
            const latestTagDisplay = repo.latestTag || "-";
            return `
                <div class="repo-item ${selected}" data-repo-name="${escapeHtml(repo.name)}">
                    <div class="repo-main">
                        <div class="repo-title">
                            <span class="repo-name">${escapeHtml(repo.name)}</span>
                            <span class="repo-status ${statusClass}">${statusText}</span>
                        </div>
                        <div class="repo-pending status" aria-label="当前分枝">br: ${escapeHtml(currentBranchDisplay)}</div>
                        <div class="repo-pending status" aria-label="最新标签">tag: ${escapeHtml(latestTagDisplay)}</div>
                        <div class="repo-pending ${pendingClass}" aria-label="已提交未推送数量">U: ↑${escapeHtml(unpushedCommitCount)}</div>
                        <div class="repo-copy">${escapeHtml(copy)}</div>
                    </div>
                    <button class="repo-delete" data-repo-name="${escapeHtml(repo.name)}" aria-label="删除仓库">x</button>
                </div>
            `;
        }).join("");
    }

    function renderConfig() {
        const repo = currentRepo();
        const disabled = !repo || isBusy;
        toggleFormDisabled(disabled);

        if (!repo) {
            elements.repoName.value = "";
            elements.modulePath.value = "";
            elements.repoUrl.value = "";
            elements.localDir.value = "";
            elements.releaseBranch.innerHTML = `<option value="">请先选择仓库</option>`;
            elements.dependencyPicker.innerHTML = `<div class="empty-inline">暂无可选依赖</div>`;
            elements.scanStatus.textContent = "未选择仓库";
            elements.currentBranch.textContent = "-";
            elements.latestTag.textContent = "-";
            elements.downstreamCount.textContent = "0";
            elements.dirtyState.textContent = "-";
            elements.newTag.value = "";
            elements.runGoModTidy.disabled = true;
            elements.showGitActionModal.disabled = true;
            elements.pushUnpushedTags.disabled = true;
            syncWorkspaceStateTone(null);
            lastSelectedRepo = "";
            return;
        }

        elements.repoName.value = repo.name || "";
        elements.modulePath.value = repo.modulePath || "";
        elements.repoUrl.value = repo.remoteUrl || "";
        elements.localDir.value = repo.localDir || "";
        renderBranchOptions(repo);
        renderDependencyPicker(repo);
        elements.scanStatus.textContent = buildScanStatus(repo);
        elements.currentBranch.textContent = repo.currentBranch || "-";
        elements.latestTag.textContent = repo.latestTag || "-";
        elements.downstreamCount.textContent = String(repo.cascadeTargets?.length || 0);
        elements.dirtyState.textContent = repo.dirty ? "有未提交改动" : "干净";
        elements.runGoModTidy.disabled = disabled || !repo.hasGoMod || !repo.isGitRepo;
        elements.showGitActionModal.disabled = disabled || !repo.isGitRepo;
        elements.pushUnpushedTags.disabled = disabled || !repo.isGitRepo || !Array.isArray(repo.tags) || repo.tags.length === 0;
        syncWorkspaceStateTone(repo);

        if (lastSelectedRepo !== repo.name) {
            elements.newTag.value = suggestNextTag(repo.latestTag);
            elements.deleteRemoteTag.checked = false;
            lastDeleteTag = repo.latestTag || repo.tags?.[0] || "";
            lastSelectedRepo = repo.name;
        }
    }

    function renderBranchOptions(repo) {
        const branches = Array.isArray(repo.branches) ? [...repo.branches] : [];
        const selected = repo.releaseBranch || repo.currentBranch || "";
        if (selected && !branches.includes(selected)) {
            branches.unshift(selected);
        }

        const options = [`<option value="">跟随当前分支</option>`];
        if (branches.length === 0) {
            options.push(`<option value="">未扫描到分支</option>`);
        } else {
            branches.forEach((branch) => {
                const isSelected = branch === selected ? "selected" : "";
                options.push(`<option value="${escapeHtml(branch)}" ${isSelected}>${escapeHtml(branch)}</option>`);
            });
        }
        elements.releaseBranch.innerHTML = options.join("");
    }

    function renderDependencyPicker(repo) {
        const candidates = appState.repos.filter((item) => item.name !== repo.name);
        if (candidates.length === 0) {
            elements.dependencyPicker.innerHTML = `<div class="empty-inline">暂无其他仓库</div>`;
            return;
        }

        elements.dependencyPicker.innerHTML = candidates.map((item) => {
            const checked = Array.isArray(repo.dependencies) && repo.dependencies.includes(item.name) ? "checked" : "";
            return `
                <label class="dependency-option">
                    <input type="checkbox" value="${escapeHtml(item.name)}" ${checked} ${isBusy ? "disabled" : ""}>
                    <span>${escapeHtml(item.name)}</span>
                </label>
            `;
        }).join("");
    }

    function renderImpact() {
        const repo = currentRepo();
        if (!repo || !Array.isArray(repo.cascadeTargets) || repo.cascadeTargets.length === 0) {
            elements.impactCount.textContent = "0 个仓库";
            elements.impactList.innerHTML = `<div class="empty-inline">当前仓库没有下游联动目标。</div>`;
            return;
        }

        elements.impactCount.textContent = `${repo.cascadeTargets.length} 个仓库`;
        elements.impactList.innerHTML = repo.cascadeTargets.map((name) => `
            <span class="impact-chip">${escapeHtml(name)}</span>
        `).join("");
    }

    function renderTags() {
        const repo = currentRepo();
        const tags = Array.isArray(repo?.tags) ? repo.tags : [];

        if (!repo || tags.length === 0) {
            elements.deleteTagName.innerHTML = `<option value="">暂无标签</option>`;
            elements.deleteTagName.disabled = true;
            elements.deleteRemoteTag.disabled = true;
            elements.deleteTag.disabled = true;
            lastDeleteTag = "";
            return;
        }

        if (!tags.includes(lastDeleteTag)) {
            lastDeleteTag = repo.latestTag && tags.includes(repo.latestTag) ? repo.latestTag : tags[0];
        }

        elements.deleteTagName.innerHTML = tags.map((tag) => {
            const selected = tag === lastDeleteTag ? "selected" : "";
            return `<option value="${escapeHtml(tag)}" ${selected}>${escapeHtml(tag)}</option>`;
        }).join("");
        elements.deleteTagName.disabled = isBusy;
        elements.deleteRemoteTag.disabled = isBusy;
        elements.deleteTag.disabled = isBusy;
    }

    function renderLogs() {
        const logs = Array.isArray(appState.logs) ? appState.logs : [];
        const wasNearBottom = elements.log.scrollHeight - elements.log.scrollTop - elements.log.clientHeight < 24;

        if (logs.length === 0) {
            elements.log.innerHTML = `<div class="log-entry">暂无日志记录</div>`;
            return;
        }

        elements.log.innerHTML = logs.map((entry) => {
            const time = escapeHtml(entry.time || "--");
            const level = escapeHtml((entry.level || "info").toUpperCase());
            const message = escapeHtml(entry.message || "");
            return `<div class="log-entry">${time} [${level}] ${message}</div>`;
        }).join("");

        if (wasNearBottom || isBusy) {
            elements.log.scrollTop = elements.log.scrollHeight;
        }
    }

    function renderReleaseSummary(response) {
        const failureReason = response?.ok === false
            ? (response?.message || lastMessage || "未知错误")
            : "";

        if (!lastRelease) {
            const summary = failureReason
                ? `失败原因: ${failureReason}`
                : (lastMessage || "暂无发布结果");
            elements.releaseSummary.textContent = summary;
            elements.releaseSummary.title = summary;
            return;
        }

        const steps = Array.isArray(lastRelease.steps) ? lastRelease.steps : [];
        const prefix = failureReason ? "失败" : "完成";
        const detail = steps.map((step) => `${step.repoName}:${step.newTag}`).join(" | ");
        const summary = failureReason
            ? `${prefix} ${lastRelease.rootRepo || "-"} -> ${lastRelease.rootTag || "-"} | 原因: ${failureReason}${detail ? ` | 已完成: ${detail}` : ""}`
            : `${prefix} ${lastRelease.rootRepo || "-"} -> ${lastRelease.rootTag || "-"}${detail ? ` | ${detail}` : ""}`;
        elements.releaseSummary.textContent = summary;
        elements.releaseSummary.title = summary;
    }

    function toggleFormDisabled(disabled) {
        elements.showAddRepo.disabled = isBusy;
        elements.refreshRepos.disabled = isBusy;
        elements.modulePath.disabled = disabled;
        elements.repoUrl.disabled = disabled;
        elements.localDir.disabled = disabled;
        elements.releaseBranch.disabled = disabled;
        elements.saveCurrentRepo.disabled = disabled;
        elements.newTag.disabled = disabled;
        elements.pushRemote.disabled = disabled;
        elements.createRelease.disabled = disabled;
    }

    function buildScanStatus(repo) {
        if (repo.lastScanError) {
            return repo.lastScanError;
        }
        if (!repo.exists) {
            return "本地目录未配置或不存在";
        }
        if (!repo.isGitRepo) {
            return "当前目录不是 Git 仓库";
        }
        if (repo.dirty) {
            return "工作区存在未提交改动";
        }
        return "扫描完成，可执行发布";
    }

    function suggestNextTag(latestTag) {
        const match = /^v(\d+)\.(\d+)\.(\d+)$/.exec(latestTag || "");
        if (!match) {
            return "v1.0.0";
        }
        return `v${match[1]}.${match[2]}.${Number(match[3]) + 1}`;
    }

    function openAddRepoModal() {
        if (isBusy) {
            return;
        }
        elements.newRepoName.value = "";
        elements.addRepoModal.style.display = "flex";
        elements.newRepoName.focus();
    }

    function closeAddRepoModal() {
        if (isBusy) {
            return;
        }
        elements.addRepoModal.style.display = "none";
    }

    function openGitActionModal() {
        const repo = currentRepo();
        if (isBusy || !repo || !repo.isGitRepo) {
            return;
        }
        elements.gitCommitMessage.value = "";
        elements.gitActionModal.style.display = "flex";
        elements.gitCommitMessage.focus();
    }

    function closeGitActionModal() {
        if (isBusy) {
            return;
        }
        elements.gitActionModal.style.display = "none";
    }

    function openConfirmModal(options) {
        if (isBusy || !options || typeof options.onConfirm !== "function") {
            return;
        }
        pendingConfirmAction = options.onConfirm;
        elements.confirmActionTitle.textContent = options.title || "操作确认";
        elements.confirmActionBody.innerHTML = options.html || "";
        elements.confirmActionButton.textContent = options.confirmText || "确认执行";
        elements.confirmActionModal.style.display = "flex";
    }

    function closeConfirmModal() {
        if (isBusy) {
            return;
        }
        pendingConfirmAction = null;
        elements.confirmActionTitle.textContent = "操作确认";
        elements.confirmActionBody.innerHTML = "";
        elements.confirmActionButton.textContent = "确认执行";
        elements.confirmActionModal.style.display = "none";
    }

    function submitConfirmAction() {
        if (isBusy || typeof pendingConfirmAction !== "function") {
            return;
        }
        const action = pendingConfirmAction;
        pendingConfirmAction = null;
        elements.confirmActionModal.style.display = "none";
        elements.confirmActionTitle.textContent = "操作确认";
        elements.confirmActionBody.innerHTML = "";
        elements.confirmActionButton.textContent = "确认执行";
        action();
    }

    function buildConfirmInfoItem(label, value) {
        return `
            <div class="confirm-item">
                <span class="confirm-label">${escapeHtml(label)}</span>
                <strong class="confirm-value">${escapeHtml(value)}</strong>
            </div>
        `;
    }

    function buildCascadePreview(repo, tag, pushRemote) {
        const cascadeTargets = Array.isArray(repo?.cascadeTargets) ? repo.cascadeTargets : [];
        const tags = Array.isArray(repo?.tags) ? repo.tags : [];
        const rootAction = tags.includes(tag) ? "复用历史标签" : "创建新标签";
        const cascadeHTML = cascadeTargets.length > 0
            ? cascadeTargets.map((name) => `<span class="impact-chip">${escapeHtml(name)}</span>`).join("")
            : `<div class="empty-inline">当前仓库没有下游联动目标，将只处理当前仓库。</div>`;

        return `
            <p class="confirm-copy">请确认本次创建标签与级联联动范围，确认后会按依赖链逐层执行。</p>
            <div class="confirm-grid">
                ${buildConfirmInfoItem("根仓库", repo?.name || "-")}
                ${buildConfirmInfoItem("标签", tag || "-")}
                ${buildConfirmInfoItem("处理方式", rootAction)}
                ${buildConfirmInfoItem("远端推送", pushRemote ? "推送到 origin" : "仅本地执行")}
            </div>
            <div class="confirm-section">
                <div class="confirm-section-title">关联的级联仓库</div>
                <div class="confirm-chip-list">${cascadeHTML}</div>
            </div>
        `;
    }

    function buildDeleteTagPreview(repo, tag, deleteRemote) {
        return `
            <p class="confirm-copy">请确认要删除的标签信息，确认后会立即执行删除操作。</p>
            <div class="confirm-grid">
                ${buildConfirmInfoItem("仓库", repo?.name || "-")}
                ${buildConfirmInfoItem("标签", tag || "-")}
                ${buildConfirmInfoItem("删除范围", deleteRemote ? "本地 + origin" : "仅本地")}
                ${buildConfirmInfoItem("当前最新标签", repo?.latestTag || "-")}
            </div>
        `;
    }

    function confirmAddRepo() {
        const name = elements.newRepoName.value.trim();
        if (!name) {
            lastMessage = "仓库名称不能为空";
            renderReleaseSummary({ ok: false });
            return;
        }
        if (appState.repos.some((repo) => repo.name === name)) {
            lastMessage = "仓库名称已存在";
            renderReleaseSummary({ ok: false });
            return;
        }

        startAsyncAction("save-repo", {
            name,
            remoteUrl: "",
            localDir: "",
            modulePath: "",
            releaseBranch: "",
            dependencies: [],
            dependenciesManual: false
        }, "正在创建仓库...", () => {
            elements.addRepoModal.style.display = "none";
        });
    }

    function runGoModTidy() {
        const repo = currentRepo();
        if (!repo) {
            lastMessage = "请先选择仓库";
            renderReleaseSummary({ ok: false });
            return;
        }
        if (!repo.hasGoMod) {
            lastMessage = "当前仓库缺少 go.mod，无法执行 go mod tidy";
            renderReleaseSummary({ ok: false });
            return;
        }

        startAsyncAction("go-mod-tidy", {
            repoName: repo.name
        }, "正在执行 go mod tidy...");
    }

    function submitGitCommit() {
        const repo = currentRepo();
        if (!repo) {
            lastMessage = "请先选择仓库";
            renderReleaseSummary({ ok: false });
            return;
        }

        const message = elements.gitCommitMessage.value.trim();
        if (!message) {
            lastMessage = "请填写提交信息";
            renderReleaseSummary({ ok: false });
            return;
        }

        startAsyncAction("git-commit", {
            repoName: repo.name,
            message
        }, "正在提交当前改动...");
    }

    function pushCurrentBranch() {
        const repo = currentRepo();
        if (!repo) {
            lastMessage = "请先选择仓库";
            renderReleaseSummary({ ok: false });
            return;
        }

        startAsyncAction("git-push", {
            repoName: repo.name
        }, "正在推送当前分支...");
    }

    function triggerPushUnpushedTags() {
        const repo = currentRepo();
        if (!repo) {
            lastMessage = "请先选择仓库";
            renderReleaseSummary({ ok: false });
            return;
        }
        if (!Array.isArray(repo.tags) || repo.tags.length === 0) {
            lastMessage = "当前仓库没有可推送的本地标签";
            renderReleaseSummary({ ok: false });
            return;
        }

        startAsyncAction("push-unpushed-tags", {
            repoName: repo.name
        }, "正在推送未推送本地标签...");
    }

    function saveCurrentRepo() {
        const repo = currentRepo();
        if (!repo) {
            lastMessage = "请先选择仓库";
            renderReleaseSummary({ ok: false });
            return;
        }

        const dependencies = [...elements.dependencyPicker.querySelectorAll('input[type="checkbox"]:checked')]
            .map((input) => input.value);

        startAsyncAction("save-repo", {
            name: repo.name,
            remoteUrl: elements.repoUrl.value.trim(),
            localDir: elements.localDir.value.trim(),
            modulePath: elements.modulePath.value.trim(),
            releaseBranch: elements.releaseBranch.value.trim(),
            dependencies,
            dependenciesManual: true
        }, "正在保存配置...");
    }

    function triggerRelease() {
        const repo = currentRepo();
        if (!repo) {
            lastMessage = "请先选择仓库";
            renderReleaseSummary({ ok: false });
            return;
        }

        const tag = elements.newTag.value.trim();
        if (!/^v\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?$/.test(tag)) {
            lastMessage = "标签格式必须类似 v1.2.3";
            renderReleaseSummary({ ok: false });
            return;
        }
        const pushRemote = elements.pushRemote.checked;
        openConfirmModal({
            title: "确认创建标签并联动下游",
            confirmText: "确认执行发布",
            html: buildCascadePreview(repo, tag, pushRemote),
            onConfirm: () => {
                startAsyncAction("create-release", {
                    repoName: repo.name,
                    tag,
                    pushRemote
                }, "正在执行级联发布...");
            }
        });
    }

    function triggerDeleteTag() {
        const repo = currentRepo();
        if (!repo) {
            lastMessage = "请先选择仓库";
            renderReleaseSummary({ ok: false });
            return;
        }

        const tag = elements.deleteTagName.value.trim();
        if (!tag) {
            lastMessage = "请先选择要删除的标签";
            renderReleaseSummary({ ok: false });
            return;
        }

        const deleteRemote = elements.deleteRemoteTag.checked;
        openConfirmModal({
            title: "确认删除标签",
            confirmText: "确认删除标签",
            html: buildDeleteTagPreview(repo, tag, deleteRemote),
            onConfirm: () => {
                startAsyncAction("delete-tag", {
                    repoName: repo.name,
                    tag,
                    deleteRemote
                }, "正在删除标签...");
            }
        });
    }

    function refreshRepos() {
        startAsyncAction("refresh-repos", undefined, "正在扫描仓库...");
    }

    function selectRepo(name) {
        startAsyncAction("select-repo", { name }, "正在切换仓库...");
    }

    function deleteRepo(name) {
        if (!window.confirm(`确认删除仓库【${name}】吗？`)) {
            return;
        }
        startAsyncAction("delete-repo", { name }, "正在删除仓库...");
    }

    function handleOperationFinished(event) {
        if (event?.release) {
            lastRelease = event.release;
        } else if (event?.action === "create-release") {
            lastRelease = null;
        } else if (["go-mod-tidy", "git-commit", "git-push", "push-unpushed-tags"].includes(event?.action)) {
            lastRelease = null;
        }
        if (event?.message) {
            lastMessage = event.message;
        }
        if (event?.action === "create-release" || event?.action === "refresh-repos") {
            lastSelectedRepo = "";
        }
        setBusy(false);
        renderReleaseSummary(event);
    }

    function escapeHtml(value) {
        return String(value)
            .replaceAll("&", "&amp;")
            .replaceAll("<", "&lt;")
            .replaceAll(">", "&gt;")
            .replaceAll('"', "&quot;")
            .replaceAll("'", "&#39;");
    }

    elements.min.onclick = () => ipc.emit("min");
    elements.close.onclick = () => ipc.emit("close");
    elements.showAddRepo.onclick = openAddRepoModal;
    elements.closeAddRepoModal.onclick = closeAddRepoModal;
    elements.confirmAddRepo.onclick = confirmAddRepo;
    elements.showGitActionModal.onclick = openGitActionModal;
    elements.closeGitActionModal.onclick = closeGitActionModal;
    elements.cancelConfirmAction.onclick = closeConfirmModal;
    elements.confirmActionButton.onclick = submitConfirmAction;
    elements.confirmGitCommit.onclick = submitGitCommit;
    elements.confirmGitPush.onclick = pushCurrentBranch;
    elements.refreshRepos.onclick = refreshRepos;
    elements.saveCurrentRepo.onclick = saveCurrentRepo;
    elements.createRelease.onclick = triggerRelease;
    elements.pushUnpushedTags.onclick = triggerPushUnpushedTags;
    elements.deleteTag.onclick = triggerDeleteTag;
    elements.runGoModTidy.onclick = runGoModTidy;

    elements.newRepoName.addEventListener("keydown", (event) => {
        if (event.key === "Enter") {
            confirmAddRepo();
        }
    });

    elements.gitCommitMessage.addEventListener("keydown", (event) => {
        if (event.key === "Enter") {
            event.preventDefault();
            submitGitCommit();
        }
    });

    elements.confirmActionModal.addEventListener("click", (event) => {
        if (event.target === elements.confirmActionModal) {
            closeConfirmModal();
        }
    });

    elements.deleteTagName.addEventListener("change", () => {
        lastDeleteTag = elements.deleteTagName.value;
    });

    elements.repoList.onclick = (event) => {
        if (isBusy) {
            return;
        }
        const deleteButton = event.target.closest(".repo-delete");
        if (deleteButton) {
            deleteRepo(deleteButton.dataset.repoName);
            return;
        }
        const repoItem = event.target.closest(".repo-item");
        if (repoItem) {
            selectRepo(repoItem.dataset.repoName);
        }
    };

    ipc.on("app-state", (nextState) => {
        applyState(nextState);
    });

    ipc.on("log-entry", (entry) => {
        appendLogEntry(entry);
    });

    ipc.on("operation-finished", (event) => {
        handleOperationFinished(event || {});
    });

    ipc.on("page-load-end", () => {
        lastMessage = "页面资源加载完成";
        renderReleaseSummary();
    });

    if (window.energy?.drag?.setup) {
        window.energy.drag.setup();
    }

    render();
    startAsyncAction("page-ready", undefined, "正在加载项目状态...");
};
