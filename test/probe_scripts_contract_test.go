package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
)

var probeScriptNames = []string{
	"test_socks5.sh",
	"test_http_https.sh",
}

func probeContractRoot() string {
	if root := os.Getenv("GEOPROXY_PROBE_CONTRACT_ROOT"); root != "" {
		return root
	}
	return "."
}

func readProbeContractFile(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(probeContractRoot(), name)
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 %s 失败: %v", path, err)
	}
	return string(contents)
}

func TestProbeScriptsUseConfigurableMultiTargetRounds(t *testing.T) {
	defaultTargetsPattern := regexp.MustCompile(`(?s)DEFAULT_TARGETS=\((.*?)\)`) // 只解析受控的 Bash 数组字面量。
	quotedURLPattern := regexp.MustCompile(`"(https://[^"[:space:]]+)"`)
	fallbackLoopPattern := regexp.MustCompile(`for\s*\(\(\s*attempt\s*=\s*0;\s*attempt\s*<\s*\$\{#TARGETS\[@\]\};\s*attempt\+\+\s*\)\)`)

	for _, name := range probeScriptNames {
		name := name
		t.Run(name, func(t *testing.T) {
			script := readProbeContractFile(t, name)

			if !strings.Contains(script, "set -euo pipefail") {
				t.Error("脚本必须启用严格错误处理 set -euo pipefail")
			}
			if !strings.Contains(script, "GEOPROXY_PROBE_URLS") {
				t.Error("脚本必须允许通过 GEOPROXY_PROBE_URLS 注入目标")
			}
			if strings.Contains(script, "httpbin.org") {
				t.Error("脚本不得继续硬编码 httpbin.org")
			}

			block := defaultTargetsPattern.FindStringSubmatch(script)
			if len(block) != 2 {
				t.Fatal("脚本必须声明 DEFAULT_TARGETS Bash 数组")
			}
			matches := quotedURLPattern.FindAllStringSubmatch(block[1], -1)
			if len(matches) < 2 {
				t.Fatalf("默认目标至少需要两个，实际为 %d", len(matches))
			}
			seen := make(map[string]struct{}, len(matches))
			for _, match := range matches {
				if _, exists := seen[match[1]]; exists {
					t.Fatalf("默认目标不能重复: %s", match[1])
				}
				seen[match[1]] = struct{}{}
			}

			if !strings.Contains(script, "probe_round()") {
				t.Error("脚本必须用 probe_round 明确一轮探针边界")
			}
			if !fallbackLoopPattern.MatchString(script) {
				t.Error("每轮必须有界遍历全部 TARGETS，不能随机只探测一个目标")
			}
			if !strings.Contains(script, "全部目标失败") {
				t.Error("脚本必须明确输出一轮全部目标失败")
			}
		})
	}
}

func TestProbeScriptsRejectHTTP4xxAndFailBoundedRuns(t *testing.T) {
	statusPattern := regexp.MustCompile(`(?m)^\s*\[\[\s+"\$1"\s+=~\s+\^\[23\]\[0-9\]\[0-9\]\$\s+\]\]\s*$`)
	failedRoundPattern := regexp.MustCompile(`(?s)if\s*\(\(\s*failed_rounds\s*>\s*0\s*\)\);\s*then\s*exit\s+1`)

	for _, name := range probeScriptNames {
		name := name
		t.Run(name, func(t *testing.T) {
			script := readProbeContractFile(t, name)
			if !strings.Contains(script, "is_success_status()") || !statusPattern.MatchString(script) {
				t.Error("成功状态必须严格限制为完整的 2xx/3xx，4xx 不得算成功")
			}
			if !strings.Contains(script, "目标站失败") {
				t.Error("HTTP 非成功状态必须明确标为目标站失败")
			}
			if !strings.Contains(script, "代理链或传输失败") {
				t.Error("curl 传输错误必须与目标站 HTTP 失败分开输出")
			}
			if !failedRoundPattern.MatchString(script) {
				t.Error("有限运行只要出现全部目标失败轮次，必须以退出码 1 结束")
			}
		})
	}
}

func TestProbeReadmeDocumentsFailureContract(t *testing.T) {
	readme := readProbeContractFile(t, "README.md")
	for _, required := range []string{
		"GEOPROXY_PROBE_URLS",
		"每行一个",
		"2xx/3xx",
		"4xx",
		"全部目标失败",
		"退出码 `1`",
	} {
		if !strings.Contains(readme, required) {
			t.Errorf("README 缺少探针合同说明 %q", required)
		}
	}
	if strings.Contains(readme, "httpbin.org") {
		t.Error("README 不得继续把 httpbin.org 描述为固定探针")
	}
}

type probeBashRuntime struct {
	executable   string
	prefix       []string
	convertPaths bool
}

func requireProbeBashRuntime(t *testing.T) probeBashRuntime {
	t.Helper()

	var bashRuntime probeBashRuntime
	switch runtime.GOOS {
	case "windows":
		executable, err := exec.LookPath("wsl.exe")
		if err != nil {
			t.Fatalf("离线行为测试环境失败: 找不到 wsl.exe: %v", err)
		}
		bashRuntime = probeBashRuntime{
			executable:   executable,
			prefix:       []string{"-d", "Debian", "--exec", "/bin/bash"},
			convertPaths: true,
		}
	case "linux":
		if _, err := os.Stat("/bin/bash"); err != nil {
			t.Fatalf("离线行为测试环境失败: /bin/bash 不可用: %v", err)
		}
		bashRuntime = probeBashRuntime{executable: "/bin/bash"}
	default:
		t.Fatalf("离线行为测试环境失败: 不支持操作系统 %s，需要 Windows+WSL Debian 或 Linux", runtime.GOOS)
		return probeBashRuntime{}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	args := append(append([]string{}, bashRuntime.prefix...), "--version")
	command := exec.CommandContext(ctx, bashRuntime.executable, args...)
	output, err := command.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("离线行为测试环境失败: Bash 运行时检查超时: %v", ctx.Err())
	}
	if err != nil {
		t.Fatalf("离线行为测试环境失败: Bash 运行时检查失败: %v\n%s", err, output)
	}
	return bashRuntime
}

func stageProbeBehaviorFiles(t *testing.T) (string, map[string]string) {
	t.Helper()

	sourceRoot := probeContractRoot()
	stageRoot := t.TempDir()
	scriptPaths := make(map[string]string, len(probeScriptNames))
	files := append([]string{}, probeScriptNames...)
	files = append(files, filepath.Join("testdata", "probe_scripts_behavior.sh"))

	var harnessPath string
	for _, name := range files {
		contents, err := os.ReadFile(filepath.Join(sourceRoot, name))
		if err != nil {
			t.Fatalf("读取行为测试文件 %s 失败: %v", name, err)
		}
		contents = bytes.ReplaceAll(contents, []byte("\r\n"), []byte("\n"))
		stagedPath := filepath.Join(stageRoot, filepath.Base(name))
		if err := os.WriteFile(stagedPath, contents, 0o755); err != nil {
			t.Fatalf("分发行为测试文件 %s 失败: %v", name, err)
		}
		if filepath.Base(name) == "probe_scripts_behavior.sh" {
			harnessPath = stagedPath
		} else {
			scriptPaths[name] = stagedPath
		}
	}
	return harnessPath, scriptPaths
}

func windowsPathToWSL(t *testing.T, path string) string {
	t.Helper()

	absolutePath, err := filepath.Abs(path)
	if err != nil {
		t.Fatalf("解析 Windows 路径 %s 失败: %v", path, err)
	}
	volume := filepath.VolumeName(absolutePath)
	if len(volume) != 2 || volume[1] != ':' {
		t.Fatalf("离线行为测试不支持非盘符 Windows 路径: %s", absolutePath)
	}
	remainder := filepath.ToSlash(strings.TrimPrefix(absolutePath, volume))
	return "/mnt/" + strings.ToLower(volume[:1]) + remainder
}

func (bashRuntime probeBashRuntime) behaviorCommand(
	t *testing.T,
	ctx context.Context,
	harnessPath string,
	scriptPath string,
	scenario string,
) *exec.Cmd {
	t.Helper()

	if bashRuntime.convertPaths {
		harnessPath = windowsPathToWSL(t, harnessPath)
		scriptPath = windowsPathToWSL(t, scriptPath)
	}
	args := append(append([]string{}, bashRuntime.prefix...), harnessPath, scriptPath, scenario)
	return exec.CommandContext(ctx, bashRuntime.executable, args...)
}

func TestProbeScriptsBehaviorWithFakeCurl(t *testing.T) {
	bashRuntime := requireProbeBashRuntime(t)
	harnessPath, scriptPaths := stageProbeBehaviorFiles(t)
	scenarios := []string{
		"fallback_2xx",
		"fallback_3xx",
		"http_fail",
		"transport_fail",
		"invalid_target",
		"invalid_count",
		"unknown_target",
	}

	for _, scriptName := range probeScriptNames {
		scriptName := scriptName
		for _, scenario := range scenarios {
			scenario := scenario
			t.Run(scriptName+"/"+scenario, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()

				command := bashRuntime.behaviorCommand(
					t,
					ctx,
					harnessPath,
					scriptPaths[scriptName],
					scenario,
				)
				output, err := command.CombinedOutput()
				if ctx.Err() != nil {
					t.Fatalf("%s/%s 离线行为测试超时: %v\n%s", scriptName, scenario, ctx.Err(), output)
				}
				if err != nil {
					t.Fatalf("%s/%s 离线行为测试失败: %v\n%s", scriptName, scenario, err, output)
				}
				if !bytes.Contains(output, []byte("PASS "+scriptName+" "+scenario+" ")) {
					t.Fatalf("%s/%s 未输出 fake-curl PASS 标记:\n%s", scriptName, scenario, output)
				}
			})
		}
	}
}
