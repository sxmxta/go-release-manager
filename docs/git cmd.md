
同步远程标签

git fetch --tags

查看标签

git tag -n

创建标签和推送

git tag v1.0.1

git push origin v1.0.1

一次性推送所有本地标签

git push origin --tags

删除标签和仓库删除

git tag -d v3.0.0-beta

git push origin --delete v3.0.0-beta


查看最新标签

按时间

git tag -l --sort=-creatordate

按版本号

git describe --tags $(git rev-list --tags --max-count=1)