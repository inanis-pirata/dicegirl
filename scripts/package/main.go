// Command package builds release archives: one zip per target platform
// containing the CLI and bot binaries, run scripts, a bot.env template and
// empty data directories. Extract and run — no Go toolchain needed.
//
//	go run ./scripts/package                 # default targets
//	go run ./scripts/package -targets linux/amd64 -out dist
package main

import (
	"archive/zip"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"dicegirl/internal/version"
)

const (
	binCLI = "dicegirl"
	binBot = "qqbot"
)

func main() {
	outDir := flag.String("out", "dist", "output directory for archives")
	targets := flag.String("targets", "windows/amd64,linux/amd64,linux/arm64,darwin/arm64",
		"comma-separated GOOS/GOARCH targets")
	ver := flag.String("version", version.Version, "release version")
	flag.Parse()

	root, err := moduleRoot()
	if err != nil {
		fail(err)
	}

	for _, target := range strings.Split(*targets, ",") {
		target = strings.TrimSpace(target)
		if target == "" {
			continue
		}
		parts := strings.SplitN(target, "/", 2)
		if len(parts) != 2 {
			fail(fmt.Errorf("invalid target %q (want GOOS/GOARCH)", target))
		}
		goos, goarch := parts[0], parts[1]
		path, err := buildTarget(root, *outDir, goos, goarch, *ver)
		if err != nil {
			fail(err)
		}
		fmt.Println(path)
	}
}

func buildTarget(root, outDir, goos, goarch, ver string) (string, error) {
	name := fmt.Sprintf("%s-%s-%s-%s", "dicegirl", ver, goos, goarch)
	stage, err := os.MkdirTemp("", "dicegirl-pkg-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(stage)

	suffix := ""
	if goos == "windows" {
		suffix = ".exe"
	}
	files := []struct {
		name string
		body []byte
		mode os.FileMode
	}{
		{binCLI + suffix, nil, 0o755},
		{binBot + suffix, nil, 0o755},
		{"run.sh", []byte(runSH), 0o755},
		// Windows PowerShell 5.1 reads UTF-8 only when a BOM is present.
		{"run.ps1", append([]byte("\xef\xbb\xbf"), runPS1...), 0o644},
		{"bot.env.example", []byte(botEnvExample), 0o644},
		{"README.txt", []byte(readme(ver, goos, goarch)), 0o644},
		{filepath.Join("data", ".keep"), []byte(""), 0o644},
		{filepath.Join("rulebooks", ".keep"), []byte(""), 0o644},
	}
	if goos != "windows" {
		files = append(files,
			struct {
				name string
				body []byte
				mode os.FileMode
			}{"install.sh", []byte(installSH), 0o755},
			struct {
				name string
				body []byte
				mode os.FileMode
			}{"uninstall.sh", []byte(uninstallSH), 0o755},
		)
	}

	// Build the two binaries straight into the staging tree.
	for _, spec := range []struct{ out, pkg string }{
		{filepath.Join(stage, binCLI+suffix), "./cmd/dicegirl"},
		{filepath.Join(stage, binBot+suffix), "./cmd/qqbot"},
	} {
		cmd := exec.Command("go", "build", "-trimpath", "-ldflags", "-s -w", "-o", spec.out, spec.pkg)
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "GOOS="+goos, "GOARCH="+goarch, "CGO_ENABLED=0")
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("build %s for %s/%s: %w", spec.pkg, goos, goarch, err)
		}
	}

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", err
	}
	zipPath := filepath.Join(outDir, name+".zip")
	if err := writeZip(zipPath, name, stage, files); err != nil {
		return "", err
	}
	return zipPath, nil
}

func writeZip(zipPath, rootName, stage string, files []struct {
	name string
	body []byte
	mode os.FileMode
}) error {
	f, err := os.Create(zipPath)
	if err != nil {
		return err
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	defer zw.Close()

	for _, spec := range files {
		hdr := &zip.FileHeader{Name: rootName + "/" + filepath.ToSlash(spec.name)}
		hdr.Modified = time.Now()
		hdr.SetMode(spec.mode)
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			return err
		}
		body := spec.body
		if body == nil {
			body, err = os.ReadFile(filepath.Join(stage, spec.name))
			if err != nil {
				return err
			}
		}
		if _, err := w.Write(body); err != nil {
			return err
		}
	}
	return nil
}

// moduleRoot walks up from the working directory to the directory holding
// go.mod.
func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found")
		}
		dir = parent
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "打包失败：", err)
	os.Exit(1)
}

const botEnvExample = `# 复制本文件为 bot.env 并填写（bot.env 不要提交或分享）
DICEGIRL_QQ_APPID=
DICEGIRL_QQ_SECRET=

# 可选
# DICEGIRL_BOT_REPLY_UNKNOWN=false   # @ 了但不是指令时是否回复提示
# DICEGIRL_BOT_DEBUG=false           # 排障：打印全部网关事件原文、丢弃原因与 HTTP 收发明细
# DICEGIRL_BOT_LOG_FILE=/var/log/dicegirl/bot.log
#   同时写入日志文件（按 10MB 轮转，保留 3 份）；不设则只输出到控制台/journald
`

const installSH = `#!/bin/sh
# dicegirl 安装脚本（Linux + systemd）
#   sudo ./install.sh
# 安装位置：
#   /opt/dicegirl            程序
#   /var/lib/dicegirl        数据（每个群独立目录）
#   /etc/dicegirl/bot.env    凭据与配置
#   /etc/systemd/system/dicegirl.service
set -eu

if [ "$(id -u)" -ne 0 ]; then
  echo "请用 sudo 运行：sudo ./install.sh" >&2
  exit 1
fi

DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
APP=/opt/dicegirl
DATA=/var/lib/dicegirl
CONF=/etc/dicegirl
UNIT=/etc/systemd/system/dicegirl.service
SVC=dicegirl
RUNUSER=dicegirl
HAS_SYSTEMD=0
command -v systemctl >/dev/null 2>&1 && HAS_SYSTEMD=1

echo "==> 安装程序到 $APP"
if [ "$HAS_SYSTEMD" = 1 ]; then
  systemctl stop "$SVC" 2>/dev/null || true
fi
install -d -m 755 "$APP"
install -m 755 "$DIR/dicegirl" "$DIR/qqbot" "$APP/"
install -m 644 "$DIR/README.txt" "$APP/"

echo "==> 创建运行用户 $RUNUSER"
if ! id -u "$RUNUSER" >/dev/null 2>&1; then
  if command -v useradd >/dev/null 2>&1; then
    useradd --system --home "$DATA" --shell /usr/sbin/nologin "$RUNUSER"
  else
    adduser -S -H -s /sbin/nologin "$RUNUSER"
  fi
fi

echo "==> 创建数据目录 $DATA"
install -d -m 700 -o "$RUNUSER" -g "$RUNUSER" \
  "$DATA" "$DATA/data" "$DATA/botdata" \
  "$DATA/data/rulebooks" "$DATA/data/datapacks"
install -d -m 750 -o "$RUNUSER" -g "$RUNUSER" /var/log/dicegirl

echo "==> 安装配置 $CONF/bot.env"
install -d -m 750 -o root -g "$RUNUSER" "$CONF"
CREATED_ENV=0
if [ ! -f "$CONF/bot.env" ]; then
  install -m 640 -o root -g "$RUNUSER" "$DIR/bot.env.example" "$CONF/bot.env"
  CREATED_ENV=1
fi

echo "==> 安装 systemd 服务"
cat > "$UNIT" <<'UNIT_EOF'
[Unit]
Description=dicegirl QQ bot
After=network-online.target
Wants=network-online.target
StartLimitIntervalSec=60
StartLimitBurst=5

[Service]
Type=simple
User=dicegirl
Group=dicegirl
WorkingDirectory=/opt/dicegirl
EnvironmentFile=/etc/dicegirl/bot.env
Environment=DICEGIRL_DATA_ROOT=/var/lib/dicegirl/data
Environment=DICEGIRL_BOT_DATA_ROOT=/var/lib/dicegirl/botdata
Environment=GOTRACEBACK=all
ExecStart=/opt/dicegirl/qqbot
Restart=always
RestartSec=5
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
ReadWritePaths=/var/lib/dicegirl /var/log/dicegirl

[Install]
WantedBy=multi-user.target
UNIT_EOF
chmod 644 "$UNIT"

if [ "$HAS_SYSTEMD" = 1 ]; then
  systemctl daemon-reload
  systemctl enable "$SVC" >/dev/null 2>&1 || true
  if [ "$CREATED_ENV" = 1 ] ||
     ! grep -Eq '^DICEGIRL_QQ_APPID=.+' "$CONF/bot.env" ||
     ! grep -Eq '^DICEGIRL_QQ_SECRET=.+' "$CONF/bot.env"; then
    echo
    echo "安装完成，但机器人凭据尚未填写。"
    echo "请编辑 $CONF/bot.env 填写 DICEGIRL_QQ_APPID 与 DICEGIRL_QQ_SECRET，然后："
    echo "  sudo systemctl start $SVC"
    exit 0
  fi
  systemctl restart "$SVC" || systemctl start "$SVC"
  echo "已启动 $SVC，查看日志：journalctl -u $SVC -f"
else
  echo "未检测到 systemd，可手动运行："
  echo "  DICEGIRL_DATA_ROOT=$DATA/data DICEGIRL_BOT_DATA_ROOT=$DATA/botdata $APP/qqbot"
fi
`

const uninstallSH = `#!/bin/sh
# dicegirl 卸载脚本
#   sudo ./uninstall.sh           保留数据与配置
#   sudo ./uninstall.sh --purge   连数据与配置一起删除
set -eu

if [ "$(id -u)" -ne 0 ]; then
  echo "请用 sudo 运行：sudo ./uninstall.sh [--purge]" >&2
  exit 1
fi

PURGE=0
case "${1:-}" in
  --purge) PURGE=1 ;;
  "") ;;
  *)
    echo "用法：sudo ./uninstall.sh [--purge]" >&2
    exit 2
    ;;
esac

APP=/opt/dicegirl
DATA=/var/lib/dicegirl
CONF=/etc/dicegirl
UNIT=/etc/systemd/system/dicegirl.service
SVC=dicegirl
RUNUSER=dicegirl

if command -v systemctl >/dev/null 2>&1; then
  systemctl stop "$SVC" 2>/dev/null || true
  systemctl disable "$SVC" 2>/dev/null || true
  rm -f "$UNIT"
  systemctl daemon-reload
fi

echo "==> 删除程序 $APP"
rm -rf "$APP"

if [ "$PURGE" = 1 ]; then
  echo "==> 删除数据 $DATA 与配置 $CONF"
  rm -rf "$DATA" "$CONF"
  if id -u "$RUNUSER" >/dev/null 2>&1; then
    userdel "$RUNUSER" 2>/dev/null || true
  fi
  echo "已完全卸载。"
else
  echo
  echo "程序已卸载，以下数据仍保留："
  echo "  数据：$DATA"
  echo "  配置：$CONF/bot.env"
  echo "如需全部删除：sudo ./uninstall.sh --purge"
fi
`

const runSH = `#!/bin/sh
# dicegirl 启动脚本
#   ./run.sh              启动 QQ 机器人（读取同目录 bot.env）
#   ./run.sh cli .r 3d6   使用命令行骰子
set -eu
DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
export DICEGIRL_DATA_ROOT="${DICEGIRL_DATA_ROOT:-$DIR/data}"
export DICEGIRL_BOT_DATA_ROOT="${DICEGIRL_BOT_DATA_ROOT:-$DIR/botdata}"

if [ -f "$DIR/bot.env" ]; then
  set -a
  . "$DIR/bot.env"
  set +a
fi

if [ "${1:-}" = "cli" ]; then
  shift
  exec "$DIR/dicegirl" --data-root "$DICEGIRL_DATA_ROOT" "$@"
fi

if [ -z "${DICEGIRL_QQ_APPID:-}" ] || [ -z "${DICEGIRL_QQ_SECRET:-}" ]; then
  echo "缺少机器人凭据：请复制 bot.env.example 为 bot.env 并填写 AppID/AppSecret" >&2
  exit 2
fi
exec "$DIR/qqbot"
`

const runPS1 = `# dicegirl 启动脚本
#   .\run.ps1              启动 QQ 机器人（读取同目录 bot.env）
#   .\run.ps1 cli .r 3d6   使用命令行骰子
$ErrorActionPreference = "Stop"
try { [Console]::OutputEncoding = [System.Text.Encoding]::UTF8 } catch {}
$Dir = Split-Path -Parent $MyInvocation.MyCommand.Path
if (-not $env:DICEGIRL_DATA_ROOT) { $env:DICEGIRL_DATA_ROOT = Join-Path $Dir "data" }
if (-not $env:DICEGIRL_BOT_DATA_ROOT) { $env:DICEGIRL_BOT_DATA_ROOT = Join-Path $Dir "botdata" }

$EnvFile = Join-Path $Dir "bot.env"
if (Test-Path $EnvFile) {
    Get-Content $EnvFile | ForEach-Object {
        $line = $_.Trim()
        if ($line -and -not $line.StartsWith("#")) {
            $i = $line.IndexOf("=")
            if ($i -gt 0) {
                [Environment]::SetEnvironmentVariable(
                    $line.Substring(0, $i).Trim(),
                    $line.Substring($i + 1).Trim(),
                    "Process")
            }
        }
    }
}

$Rest = $args
if ($Rest.Count -gt 0 -and $Rest[0] -eq "cli") {
    $exe = Join-Path $Dir "dicegirl.exe"
    $cliArgs = @("--data-root", $env:DICEGIRL_DATA_ROOT) + @($Rest | Select-Object -Skip 1)
    & $exe @cliArgs
    exit $LASTEXITCODE
}

if (-not $env:DICEGIRL_QQ_APPID -or -not $env:DICEGIRL_QQ_SECRET) {
    Write-Host "缺少机器人凭据：请复制 bot.env.example 为 bot.env 并填写 AppID/AppSecret" -ForegroundColor Yellow
    exit 2
}
& (Join-Path $Dir "qqbot.exe")
exit $LASTEXITCODE
`

func readme(ver, goos, goarch string) string {
	exe := ""
	if goos == "windows" {
		exe = ".exe"
	}
	run := "./run.sh"
	if goos == "windows" {
		run = `.\\run.ps1`
	}
	return fmt.Sprintf(`dicegirl %s（%s/%s）
==============================

内含
  dicegirl%s   命令行骰子 / 规则引擎
  qqbot%s      QQ 官方机器人适配器
  run.sh / run.ps1   启动脚本

Linux 服务器安装（systemd）
  sudo ./install.sh
  # 安装完成后编辑 /etc/dicegirl/bot.env 填写凭据，再：
  sudo systemctl start dicegirl
  # 查看日志：journalctl -u dicegirl -f
  # 卸载（保留数据）：sudo ./uninstall.sh
  # 完全卸载：      sudo ./uninstall.sh --purge

快速开始（免安装，直接运行）
  1. 复制 bot.env.example 为 bot.env，填写 DICEGIRL_QQ_APPID 与 DICEGIRL_QQ_SECRET
  2. 运行 %s
  3. 在 QQ 群中 @机器人，例如：
       @机器人 help
       @机器人 .r 3d6+1
       @机器人 .ra 心理学

命令行用法
  %s cli .r 3d6+1
  %s cli .ra 心理学

数据位置
  data/      CLI 的角色卡与本地状态
  botdata/   机器人数据：每个群一个独立目录（groups/<群 openid>/），
             私聊用户数据在 c2c/<用户 openid>/
  rulebooks/ 放入自定义规则书 YAML（内置 coc7、dnd5e 已嵌入程序）

说明
  - 机器人凭据只从环境变量或 bot.env 读取；不要把 bot.env 分享给别人。
  - 首次使用暗骰（.h/.rh）或围观（.ob）需要在群里按提示，私聊机器人发送绑定码。
  - 日志：systemd 安装时由 journald 保存（journalctl -u dicegirl）；
    也可在配置里设 DICEGIRL_BOT_LOG_FILE=/var/log/dicegirl/bot.log 额外写文件。
  - 跑团记录存在每个群自己的 SQLite 里；导出的 txt/html 在
    <botdata>/groups/<群>/logs/ 下（旧版 *.md 战报首次使用时会自动导入并保留）。
`, ver, goos, goarch, exe, exe, run, run, run)
}
