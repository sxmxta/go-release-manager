package app

import (
	"encoding/json"
	"fmt"
	"github.com/energye/energy/v3/ipc"
	"github.com/energye/energy/v3/ipc/callback"
	"github.com/energye/lcl/lcl"
	"sync"
)

var registerIPCOnce sync.Once

func registerIPCHandlers() {
	registerIPCOnce.Do(func() {
		ipc.On("page-ready", func(context callback.IContext) {
			startAsyncAction(context, "page-ready", func() (*ActionResponse, error) {
				return &ActionResponse{OK: true, Message: "page ready"}, nil
			})
		})

		ipc.On("save-repo", func(context callback.IContext) {
			var payload RepoPayload
			if err := decodeFirstArg(context, &payload); err != nil {
				context.Result(&ActionResponse{OK: false, Message: err.Error()})
				return
			}
			startAsyncAction(context, "save-repo", func() (*ActionResponse, error) {
				message, err := appStore.saveRepoLocked(payload)
				if err != nil {
					return nil, err
				}
				return &ActionResponse{OK: true, Message: message}, nil
			})
		})

		ipc.On("delete-repo", func(context callback.IContext) {
			var payload DeleteRepoPayload
			if err := decodeFirstArg(context, &payload); err != nil {
				context.Result(&ActionResponse{OK: false, Message: err.Error()})
				return
			}
			startAsyncAction(context, "delete-repo", func() (*ActionResponse, error) {
				message, err := appStore.deleteRepoLocked(payload.Name)
				if err != nil {
					return nil, err
				}
				return &ActionResponse{OK: true, Message: message}, nil
			})
		})

		ipc.On("select-repo", func(context callback.IContext) {
			var payload SelectRepoPayload
			if err := decodeFirstArg(context, &payload); err != nil {
				context.Result(&ActionResponse{OK: false, Message: err.Error()})
				return
			}
			startAsyncAction(context, "select-repo", func() (*ActionResponse, error) {
				message, err := appStore.selectRepoLocked(payload.Name)
				if err != nil {
					return nil, err
				}
				return &ActionResponse{OK: true, Message: message}, nil
			})
		})

		ipc.On("delete-tag", func(context callback.IContext) {
			var payload DeleteTagPayload
			if err := decodeFirstArg(context, &payload); err != nil {
				context.Result(&ActionResponse{OK: false, Message: err.Error()})
				return
			}
			startAsyncAction(context, "delete-tag", func() (*ActionResponse, error) {
				message, err := appStore.deleteTagLocked(payload)
				if err != nil {
					return nil, err
				}
				return &ActionResponse{OK: true, Message: message}, nil
			})
		})

		ipc.On("go-mod-tidy", func(context callback.IContext) {
			var payload RepoActionPayload
			if err := decodeFirstArg(context, &payload); err != nil {
				context.Result(&ActionResponse{OK: false, Message: err.Error()})
				return
			}
			startAsyncAction(context, "go-mod-tidy", func() (*ActionResponse, error) {
				message, err := appStore.goModTidyLocked(payload)
				if err != nil {
					return nil, err
				}
				return &ActionResponse{OK: true, Message: message}, nil
			})
		})

		ipc.On("git-commit", func(context callback.IContext) {
			var payload GitCommitPayload
			if err := decodeFirstArg(context, &payload); err != nil {
				context.Result(&ActionResponse{OK: false, Message: err.Error()})
				return
			}
			startAsyncAction(context, "git-commit", func() (*ActionResponse, error) {
				message, err := appStore.gitCommitLocked(payload)
				if err != nil {
					return nil, err
				}
				return &ActionResponse{OK: true, Message: message}, nil
			})
		})

		ipc.On("git-push", func(context callback.IContext) {
			var payload RepoActionPayload
			if err := decodeFirstArg(context, &payload); err != nil {
				context.Result(&ActionResponse{OK: false, Message: err.Error()})
				return
			}
			startAsyncAction(context, "git-push", func() (*ActionResponse, error) {
				message, err := appStore.gitPushLocked(payload)
				if err != nil {
					return nil, err
				}
				return &ActionResponse{OK: true, Message: message}, nil
			})
		})

		ipc.On("push-unpushed-tags", func(context callback.IContext) {
			var payload RepoActionPayload
			if err := decodeFirstArg(context, &payload); err != nil {
				context.Result(&ActionResponse{OK: false, Message: err.Error()})
				return
			}
			startAsyncAction(context, "push-unpushed-tags", func() (*ActionResponse, error) {
				message, err := appStore.pushUnpushedTagsLocked(payload)
				if err != nil {
					return nil, err
				}
				return &ActionResponse{OK: true, Message: message}, nil
			})
		})

		ipc.On("refresh-repos", func(context callback.IContext) {
			startAsyncAction(context, "refresh-repos", func() (*ActionResponse, error) {
				message, err := appStore.refreshLocked()
				if err != nil {
					return nil, err
				}
				return &ActionResponse{OK: true, Message: message}, nil
			})
		})

		ipc.On("create-release", func(context callback.IContext) {
			var payload ReleasePayload
			if err := decodeFirstArg(context, &payload); err != nil {
				context.Result(&ActionResponse{OK: false, Message: err.Error()})
				return
			}
			startAsyncAction(context, "create-release", func() (*ActionResponse, error) {
				result, message, err := appStore.executeReleaseLocked(payload)
				if err != nil {
					return &ActionResponse{
						OK:      false,
						Message: err.Error(),
						Release: result,
					}, err
				}
				return &ActionResponse{
					OK:      true,
					Message: message,
					Release: result,
				}, nil
			})
		})
	})
}

func PageLoadEnd() {
	lcl.RunOnMainThreadAsync(func(id uint32) {
		ipc.Emit("page-load-end")
	})
}

func startAsyncAction(context callback.IContext, action string, fn func() (*ActionResponse, error)) {
	browserID := context.BrowserId()
	appStore.runAsync(browserID, action, fn)
	context.Result(&ActionResponse{
		OK:      true,
		Message: "accepted",
	})
}

func decodeFirstArg(context callback.IContext, target any) error {
	value, ok := firstArgument(context.Data())
	if !ok {
		return fmt.Errorf("missing argument")
	}
	switch data := value.(type) {
	case string:
		if err := json.Unmarshal([]byte(data), target); err != nil {
			return fmt.Errorf("decode argument failed: %w", err)
		}
		return nil
	default:
		raw, err := json.Marshal(data)
		if err != nil {
			return fmt.Errorf("marshal argument failed: %w", err)
		}
		if err := json.Unmarshal(raw, target); err != nil {
			return fmt.Errorf("decode argument failed: %w", err)
		}
		return nil
	}
}

func firstArgument(data any) (any, bool) {
	switch value := data.(type) {
	case []any:
		if len(value) == 0 {
			return nil, false
		}
		return value[0], true
	default:
		if value == nil {
			return nil, false
		}
		return value, true
	}
}
