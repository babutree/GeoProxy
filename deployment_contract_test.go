package main

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/babutree/GeoProxy/config"
)

type startupCountryFilterStoreStub struct {
	allowedCalls int
	blockedCalls int
	allowedErr   error
	blockedErr   error
}

func (s *startupCountryFilterStoreStub) DisableNotAllowedCountries([]string) (int64, error) {
	s.allowedCalls++
	return 0, s.allowedErr
}

func (s *startupCountryFilterStoreStub) DisableBlockedCountries([]string) (int64, error) {
	s.blockedCalls++
	return 0, s.blockedErr
}

func TestApplyStartupCountryFiltersPropagatesStorageFailure(t *testing.T) {
	wantErr := errors.New("forced country filter failure")
	tests := []struct {
		name        string
		cfg         *config.Config
		store       *startupCountryFilterStoreStub
		wantAllowed int
		wantBlocked int
	}{
		{
			name:        "allowlist",
			cfg:         &config.Config{AllowedCountries: []string{"JP"}},
			store:       &startupCountryFilterStoreStub{allowedErr: wantErr},
			wantAllowed: 1,
		},
		{
			name:        "denylist",
			cfg:         &config.Config{BlockedCountries: []string{"CN"}},
			store:       &startupCountryFilterStoreStub{blockedErr: wantErr},
			wantBlocked: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := applyStartupCountryFilters(tt.store, tt.cfg)
			if !errors.Is(err, wantErr) {
				t.Fatalf("applyStartupCountryFilters() error = %v, want %v", err, wantErr)
			}
			if tt.store.allowedCalls != tt.wantAllowed || tt.store.blockedCalls != tt.wantBlocked {
				t.Fatalf("filter calls = allowed:%d blocked:%d, want %d/%d", tt.store.allowedCalls, tt.store.blockedCalls, tt.wantAllowed, tt.wantBlocked)
			}
		})
	}
}

type healthConfigUpdaterStub struct {
	got *config.Config
}

func (s *healthConfigUpdaterStub) UpdateConfig(cfg *config.Config) {
	s.got = cfg
}

func TestConsumeConfigChangesPublishesLatestConfigToHealthChecker(t *testing.T) {
	previous := config.Get()
	t.Cleanup(func() { config.SetGlobal(previous) })

	want := &config.Config{HealthIntervalMinutes: 17, ValidateTimeout: 23}
	config.SetGlobal(want)
	changes := make(chan struct{}, 1)
	changes <- struct{}{}
	close(changes)
	updater := &healthConfigUpdaterStub{}

	consumeConfigChanges(changes, updater)
	if updater.got != want {
		t.Fatalf("UpdateConfig() cfg = %p, want latest config %p", updater.got, want)
	}
}

// Compose 首启必须透传只读 API 的三项环境配置；否则容器部署无法按公开合同配置网关地址、Key 和限流。
func TestComposeForwardsReadOnlyAPIEnvironment(t *testing.T) {
	compose, err := os.ReadFile("docker-compose.yml")
	if err != nil {
		t.Fatalf("ReadFile(docker-compose.yml) error = %v", err)
	}
	for _, required := range []string{
		"PUBLIC_HOST=${PUBLIC_HOST:-}",
		"READONLY_API_KEYS=${READONLY_API_KEYS:-}",
		"READONLY_API_RATE_PER_MIN=${READONLY_API_RATE_PER_MIN:-60}",
	} {
		if !strings.Contains(string(compose), required) {
			t.Errorf("docker-compose.yml missing environment forwarding %q", required)
		}
	}
}

// README 配置表必须公开 Compose 可用的只读 API 首启变量，避免用户只能从设计文档反推部署方式。
func TestREADMEListsReadOnlyAPIEnvironment(t *testing.T) {
	readme, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatalf("ReadFile(README.md) error = %v", err)
	}
	for _, required := range []string{"PUBLIC_HOST", "READONLY_API_KEYS", "READONLY_API_RATE_PER_MIN"} {
		if !strings.Contains(string(readme), required) {
			t.Errorf("README.md missing configuration variable %q", required)
		}
	}
}

// singBoxPinnedVersion 是本仓库锁定的 sing-box 版本。
// CI 与镜像必须用同一版本：CI 用它跑 sing-box 相关测试，运行镜像用它生成/加载配置，
// 版本漂移会让 CI 通过而线上因协议字段差异启动失败。
const singBoxPinnedVersion = "1.13.16"

// 发布工作流必须同时要求 Docker Hub 用户名与 token，且在任何镜像构建/推送前运行 Go 测试和静态检查。
func TestDockerWorkflowRequiresBothSecretsAndPreflight(t *testing.T) {
	workflow, err := os.ReadFile(".github/workflows/docker-image.yml")
	if err != nil {
		t.Fatalf("ReadFile(docker workflow) error = %v", err)
	}
	content := string(workflow)
	for _, required := range []string{
		"secrets.DOCKERHUB_USERNAME",
		"secrets.DOCKERHUB_TOKEN",
		"name: Set up Go",
		"go-version-file: go.mod",
		"name: Set up Node",
		"uses: actions/setup-node@v4",
		"node-version: 20",
		"name: Set up sing-box",
		"SINGBOX_VERSION: " + singBoxPinnedVersion,
		"install -m 0755",
		"$RUNNER_TEMP/bin/sing-box",
		"echo \"$RUNNER_TEMP/bin\" >> \"$GITHUB_PATH\"",
		"name: Run Go tests",
		"run: go test ./...",
		"name: Run Go vet",
		"run: go vet ./...",
	} {
		if !strings.Contains(content, required) {
			t.Errorf("docker workflow missing %q", required)
		}
	}
	if !strings.Contains(content, "DOCKERHUB_USERNAME }}\" != '' ] && [ \"${{ secrets.DOCKERHUB_TOKEN") {
		t.Error("Docker Hub credential check must require both username and token")
	}
	testIndex := strings.Index(content, "run: go test ./...")
	vetIndex := strings.Index(content, "run: go vet ./...")
	buildIndex := strings.Index(content, "uses: docker/build-push-action")
	if testIndex < 0 || vetIndex < 0 || buildIndex < 0 || testIndex > buildIndex || vetIndex > buildIndex {
		t.Error("Go test and vet must run before the first image build/push action")
	}
	nodeIndex := strings.Index(content, "name: Set up Node")
	singBoxIndex := strings.Index(content, "name: Set up sing-box")
	if nodeIndex < 0 || singBoxIndex < 0 || testIndex < 0 || nodeIndex > testIndex || singBoxIndex > testIndex {
		t.Error("Node and sing-box must be prepared before Go tests")
	}
}

// TestSingBoxVersionIsPinnedConsistently 锁定 CI 与运行镜像使用同一 sing-box 版本。
// 两处任一漂移都会造成「CI 用 A 版跑测试、镜像装 B 版跑生产」，
// 协议字段差异只会在线上暴露。
func TestSingBoxVersionIsPinnedConsistently(t *testing.T) {
	dockerfile, err := os.ReadFile("Dockerfile")
	if err != nil {
		t.Fatalf("ReadFile(Dockerfile) error = %v", err)
	}
	wantARG := "ARG SINGBOX_VERSION=" + singBoxPinnedVersion
	if !strings.Contains(string(dockerfile), wantARG) {
		t.Errorf("Dockerfile missing %q; CI and runtime sing-box versions must match", wantARG)
	}
	// Dockerfile 与工作流都必须按 tag 精确下载，不得回退到 latest。
	if strings.Contains(string(dockerfile), "releases/latest") {
		t.Error("Dockerfile must download a pinned sing-box tag, not latest")
	}
	workflow, err := os.ReadFile(".github/workflows/docker-image.yml")
	if err != nil {
		t.Fatalf("ReadFile(docker workflow) error = %v", err)
	}
	if strings.Contains(string(workflow), "releases/latest") {
		t.Error("docker workflow must download a pinned sing-box tag, not latest")
	}
}

// Compose 必须保留配置层“未设置默认 CN、显式空值关闭”的双态语义。
func TestComposePreservesBlockedCountryUnsetAndExplicitEmptySemantics(t *testing.T) {
	compose, err := os.ReadFile("docker-compose.yml")
	if err != nil {
		t.Fatalf("ReadFile(docker-compose.yml) error = %v", err)
	}
	content := string(compose)
	if !strings.Contains(content, "BLOCKED_COUNTRIES=${BLOCKED_COUNTRIES-CN}") {
		t.Fatal("docker-compose.yml must default BLOCKED_COUNTRIES to CN only when unset")
	}
	if strings.Contains(content, "BLOCKED_COUNTRIES=${BLOCKED_COUNTRIES:-CN}") {
		t.Fatal("docker-compose.yml must not turn an explicit empty BLOCKED_COUNTRIES into CN")
	}
}
