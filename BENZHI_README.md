# BENZHI_README

## 项目说明

- 项目：11DingKing/embodied-reading-go-tasks
- 项目用途：Embodied Reading Studio is a production-oriented Go backend for humanities study groups that preserve direct, physical reading as a first-class research process. It coordinates physical-edition registration, time-bounded page reservations, offline close-reading sessions, observation notes, quotation claims, independent review, seminar disposition, and auditable archive snapshots.
- Go 工具链：`golang:1.26.0`
- 前端工具链：无

## 标准构建、运行和测试命令

进入容器后执行：

```bash
# 编译
cd '/app' && GOTOOLCHAIN=local go build ./...

# 启动
cd '/app' && GOTOOLCHAIN=local go run ./cmd/server

# 测试
cd '/app' && GOTOOLCHAIN=local go test ./...
```

## Docker 构建和进入容器

```bash
chmod +x build_benzhi_docker.sh
./build_benzhi_docker.sh benzhi-task-21-amd64 linux/amd64
./build_benzhi_docker.sh benzhi-task-21-arm64 linux/arm64
docker run -it benzhi-task-21-amd64:latest
docker run -it --platform linux/arm64 benzhi-task-21-arm64:latest
```

## 题目验证命令

1. 预期退出码 1：`go test ./internal/service -run '^TestReviewDecisionFailurePreservesLeaseAndClaimState$' -count=1`
