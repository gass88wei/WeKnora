# AGENTS.md — WeKnora 
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
