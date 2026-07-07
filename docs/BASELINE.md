# Baseline Snapshot

Task: P0 combined cleanup baseline

Updated at: 2026-07-07

## Repository State

- Branch: `feature/geo-gateway`
- Baseline commit during verification: `6ed98dc`

## Canonical Build Environment

Run this PowerShell environment prefix before Go commands:

```powershell
$env:PATH="C:\Program Files\Go\bin;C:\ProgramData\mingw64\mingw64\bin;"+$env:PATH
$env:CGO_ENABLED='1'
$env:GOPROXY="https://goproxy.cn,direct"
$env:ALL_PROXY="socks5://10.0.1.9:7890"
```

## Toolchain

### Go

Command: `go version`

Result:

```text
go version go1.26.4 windows/amd64
```

### CGO Compiler

Command: `gcc --version`

Result:

```text
gcc.exe (x86_64-posix-seh-rev0, Built by MinGW-Builds project) 16.1.0
```

CGO compiler available: yes

## Build Verification

Command:

```powershell
go build ./...
```

Result: pass, exit code 0
