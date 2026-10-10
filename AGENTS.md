# AGENTS.md — WeKnora fork（gass88wei）协作须知

> 目标读者：任何 AI 编码代理（OpenCode / Claude Code / Cursor…）。
> 读完这一篇，你应该能在**不装 Go 工具链**的前提下，独立完成「改代码 → 出 Windows 安装包」的闭环。

---

## 1. 仓库关系与工作副本

| 项 | 值 |
|---|---|
| origin | `gass88wei/WeKnora`（fork of `Tencent/WeKnora`，基线 `v0.8.2`） |
| 本机工作副本 | `E:\008\WeKnora-0.8.2` —— **唯一**有 `.git` 的目录，改动只在这里做 |
| ⚠️ 陷阱 | `E:\009\WeKnora-0.8.2` 是解压出来的干净 tarball，**没有 `.git`**，改了不会进版本库 |
| 基线差异 | 本 fork 已叠加桌面版单二进制改造，**不要**直接 cherry-pick 上游提交 |

**不要动的东西**（fork 不适用）：
- `release-lite.yml` 的 `push: tags: v*` 触发（会和上游 tag 撞）
- `update-homebrew` job（依赖上游 tap，已注释）

---

## 2. 硬约束：本地没有工具链

本机**没有** `go` / `wails` / `makensis`。所以：

- ❌ 不要尝试本地编译、跑 `go build`、`wails build`
- ❌ 不要装 Go「顺便验证一下」——浪费时间，CI 就是验证器
- ✅ 一切编译、打包、冒烟测试都在 GitHub Actions 完成
- ✅ 本地只做：读代码、改文件、`git push`、`gh` 触发与读日志

工具：

```
gh CLI : C:\Program Files\GitHub CLI\gh.exe   （已登录 gass88wei，scope 含 workflow，可推 workflow 文件）
git    : C:\Program Files\Git\bin\git.exe
node   : C:\Program Files\nodejs\node.exe      （仅供 npx js-yaml 校验 YAML）
```

校验 workflow YAML（推之前必做，省一轮 CI）：

```powershell
npx --yes js-yaml "E:\008\WeKnora-0.8.2\.github\workflows\release-lite.yml" > $null 2>&1
if ($LASTEXITCODE -eq 0) { "YAML OK" } else { "YAML FAIL" }
```

---

## 3. 迭代循环（核心工作流）

```powershell
$gh = "C:\Program Files\GitHub CLI\gh.exe"
$repo = "gass88wei/WeKnora"

# 1) 提交推送 —— 自动触发 App + Go Lint Cache（约 3 分钟，这是第一道验证）
git -C "E:\008\WeKnora-0.8.2" add -A
git -C "E:\008\WeKnora-0.8.2" commit -m "<type>: <what>; <why>"
git -C "E:\008\WeKnora-0.8.2" push origin main

# 2) 触发打包 —— only=windows 约 10 分钟，all 约 19 分钟
& $gh workflow run release-lite.yml --repo $repo --ref main -f tag=v0.8.2 -f only=windows

# 3) 盯结果
& $gh run list --repo $repo --workflow=release-lite.yml --limit 1
& $gh run watch <RUN_ID> --repo $repo

# 4) 只看关键探测行
& $gh run view <RUN_ID> --repo $repo --log | Select-String -Pattern "UI probe|launch smoke"

# 5) 拿安装包
& $gh release view v0.8.2 --repo $repo --json url
```

**提交信息风格**（沿用仓库既有风格）：`fix:` / `feat:` / `ci:` + 一句话；正文写清 *为什么*，不写 *改了哪几行*。

### 验收标准（唯一判据）

| 现象 | 含义 |
|---|---|
| `UI probe: status=200` + `launch smoke passed (no panic)` | ✅ 通过 |
| `UI probe: status=401` + `body head:` 为空 | ❌ 前端没被打进二进制（见 §4 坑 1） |
| `requires at least 2 arg(s), only received 1` | ❌ 上游构建 job 全挂/被跳过，release 收到 0 个 artifact（见 §4 坑 4） |

**三振止损**：同一个 job 连挂 3 次就停止盲推，回去读代码定位根因。

---

## 4. 桌面版架构与 4 个坑（复刻必读）

桌面应用 = 单个进程内三件套（`cmd/desktop/main.go`）：

```
Wails WebView ──► reverse proxy ──► 内嵌 Gin 后端 (127.0.0.1:随机端口)
                                        │
                                   SQLite + 本地文件
```

免登录：进程启动时生成随机 token，前端经 `GetAutoSetupToken` 拿到后调 `POST /api/v1/auth/auto-setup` 建默认管理员。

### 坑 1（最致命）：前端靠 `//go:embed` 打进 exe

- `frontend/embed.go` 里 `//go:embed dist` → 编译时抓 `frontend/dist/`
- **git 里 `frontend/dist/` 只有 `README.txt`**（`.gitignore` 排除 `dist/*`，只留 README）
- `internal/router/static.go` 找不到 `index.html` 就**什么都不注册** → 所有非 `/api/` 请求落到 `router.go` 的 `middleware.Auth` → **裸 401**
- wails.json 故意没有 `frontend:build`，CI 日志会显示 `No Build command. Skipping.`

⇒ **任何**改构建流程的人，必须保证 `go build` 之前 `frontend/dist/` 里有 vite 产物。当前实现：`build-frontend` job 无条件跑，`build-desktop-app` 用 `download-artifact` 取回。

### 坑 2：资源全部相对 cwd

`config/config.yaml`、`migrations/sqlite/`、`web/` 都按相对路径找，`internal/config.LoadConfig` 找不到就返回 error，`container.go` 的 `must()` 直接 **panic**。

Windows 解法在 `cmd/desktop/userconfig.go` 的 `ensureDesktopUserConfig()`：把内嵌的 config 树物化到 `os.UserConfigDir()/WeKnora Lite/`，并在解析不到时 chdir 过去。**改路径逻辑前先读这个文件。**

### 坑 3：数据目录只在 macOS 有专门处理

`configureDesktopStorage()` 只处理 `.app/Contents/MacOS`，其余平台早退。Windows 的落盘依赖 `ensureDesktopUserConfig` + env（`DB_PATH` / `LOCAL_STORAGE_BASE_DIR`）。若数据跑到 `C:\Program Files\...`，说明这条链断了。

### 坑 4：release job 会在上游全挂时照样跑

```yaml
release:
  if: ${{ !cancelled() }}   # ← 关键
  needs: [build-binary, build-desktop-app]
```

上游被 `if` 跳过或失败，release 仍执行 → 0 个 artifact → bash 空数组 `"${arr[@]}"` 展开成 0 个词 → `gh release upload` 报 `requires at least 2 arg(s)`，**这个报错会盖住真因**。修复处已加空数组守卫并输出 `::error::`。

---

## 5. 关键文件地图

| 文件 | 作用 |
|---|---|
| `.github/workflows/release-lite.yml` | **唯一**打包入口。`workflow_dispatch`，输入 `tag` + `only`（`all`/`windows`） |
| `cmd/desktop/main.go` | 桌面入口：chdir 逻辑 → 物化 config → `FrontendFallbackFS = frontend.Dist()` → `BuildContainer`（顺序不能乱） |
| `cmd/desktop/userconfig.go` | 把内嵌 config 树物化到用户目录，防 panic |
| `cmd/desktop/prefs.go` | `desktop-prefs.json`：`http_port` / `http_bind_public` / `project_dirs` |
| `cmd/desktop/update.go` | GitHub Releases 自动更新（按 `GOOS/GOARCH` 选 asset） |
| `frontend/embed.go` | `//go:embed dist`，`Dist()` 无 bundle 时返回只有 README 的 FS |
| `internal/router/static.go` | `FrontendFallbackFS` + `serveFrontendStatic`；缺前端时会打 `Errorf` 警告 |
| `internal/router/router.go:170` | `if handler.Edition == "lite"` 才注册静态路由 |
| `internal/config/config.go:501` | `LoadConfig()`，找不到 config.yaml 即返回 error |
| `internal/database/migration.go:106` | 迁移路径 `file://migrations/sqlite`（相对 cwd） |
| `cmd/desktop/build/windows/installer/project.nsi` | NSIS 模板；`wails.files` 只打 exe + licenses |

---

## 6. 从零复刻（换机器 / 换 AI 时）

1. `gh repo fork Tencent/WeKnora --clone` → 得到本地 git 仓库
2. 合入本 fork 的桌面版改造（`git remote add mine <本仓库> && git fetch mine && git merge mine/main`）
3. 确认 `.github/workflows/release-lite.yml` 含三件事：
   - `build-frontend` **无 `if`**（无条件跑）
   - `build-desktop-app` 的 `needs` 含 `build-frontend`，且在 `wails build` 前有 `download-artifact` 到 `frontend/dist/`
   - release 上传有**空数组守卫**
4. `npx js-yaml` 校验 → `git push` → 看 App / Go Lint 是否绿
5. `gh workflow run release-lite.yml -f tag=vX.Y.Z -f only=windows`
6. 读 `UI probe: status=200` → 下载 `WeKnora-Lite-App_*_windows_amd64_setup.exe`

### 人工验收清单（CI 覆盖不到的最后一环）

CI 冒烟跑的是**裸 exe**，不是 NSIS 安装后的真实布局。装完必须手测：

- [ ] 双击图标 → 进主界面（**不是白屏、不是 401 JSON**）
- [ ] 数据目录落在 `%AppData%\WeKnora Lite\`，**不是** `C:\Program Files\...`
- [ ] 能建知识库 + 上传一个文档 + 完成一次问答

---

## 7. 相关历史（供追溯）

| commit | 内容 |
|---|---|
| `af57d93` | ci: 前端产物打进桌面二进制；release 空数组守卫（修 401） |
| `63825f9` | feat: SPA embedded frontend（单二进制安装） |
| `7b46a1b` | fix: 按显式路径加载物化的 .env，DB_DRIVER 才能解析 |
| `5124d7c` | fix: 首次运行物化 .env + sqlite migrations；CI launch smoke |
| `043e05d` | fix: 首次运行物化内嵌 config 树 |

对应问题记录见 Engram 记忆：`bug/weknora-401-ci-frontend-dist`、`bug/weknora-windows-4-bug`。
