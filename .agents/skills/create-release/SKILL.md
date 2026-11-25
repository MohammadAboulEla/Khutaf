---
name: create-release
description: >-
  Builds, packages, and generates changelog/release notes for a new Khutaf Windows release
  in dist/. Use whenever the user asks to create, build, or package a new release or beta version.
---

# Create Khutaf Release

Follow these steps in order whenever creating a new release of Khutaf:

## 1. Determine Version & Tag
- Confirm the version string (e.g. `0.1.2-beta` or `0.2.0`) and tag `v<version>`.
- Check existing tags/commits to scope the changelog:
  ```powershell
  git tag --sort=-creatordate | Select-Object -First 5
  git log --oneline -n 20
  ```

## 2. Bump Version in Source
- Update `var AppVersion = "<version>"` in `app.go`.
- Update `version = "<version>"` under `[workspace.package]` in `native/Cargo.toml`.

## 3. Run Verification Checks
- Run Go vet & tests, and verify the frontend builds cleanly:
  ```powershell
  go vet ./...
  go test ./...
  npm --prefix frontend run build
  ```

## 4. Build Native Hook DLL & Wails Binary
- Rebuild `runtime/khutaf-hook64.dll`:
  ```powershell
  powershell -ExecutionPolicy Bypass -File scripts/build-native.ps1
  ```
- Build `build/bin/Khutaf.exe` with `AppVersion` injected:
  ```powershell
  wails build -ldflags "-X main.AppVersion=<version>"
  ```

## 5. Package Release Zip & Checksum in `dist/`
- Keep the GitHub release tag `v<version>` identical to `AppVersion` in `app.go` (e.g. `v0.1.6-beta`) so in-app auto-update version checks stay aligned.
- Stage only `Khutaf.exe` and `runtime/khutaf-hook64.dll` (never include `runtime/TMP_Font_AssetBundles/` — users download that in-app from the `unity-fonts` release).
- Zip with `.NET ZipFile` (avoid `Compress-Archive` progress-bar errors in non-interactive shells) and write `dist/SHA256SUMS.txt` for updater integrity verification:
  ```powershell
  $stage = "E:\programing\go\Khutaf\dist\_stage"
  Remove-Item -Recurse -Force $stage -ErrorAction SilentlyContinue
  New-Item -ItemType Directory -Force "$stage\runtime" | Out-Null
  Copy-Item "build\bin\Khutaf.exe" "$stage\Khutaf.exe"
  Copy-Item "runtime\khutaf-hook64.dll" "$stage\runtime\khutaf-hook64.dll"
  $zip = "E:\programing\go\Khutaf\dist\Khutaf-v<version>-win64.zip"
  Remove-Item -Force $zip -ErrorAction SilentlyContinue
  Add-Type -AssemblyName System.IO.Compression.FileSystem
  [System.IO.Compression.ZipFile]::CreateFromDirectory($stage, $zip, [System.IO.Compression.CompressionLevel]::Optimal, $false)
  Remove-Item -Recurse -Force $stage
  $hash = (Get-FileHash $zip -Algorithm SHA256).Hash.ToLower()
  "$hash  $(Split-Path $zip -Leaf)" | Set-Content -Encoding ascii "E:\programing\go\Khutaf\dist\SHA256SUMS.txt"
  Get-Item $zip | Select-Object Name, Length
  ```

## 6. Generate Changelog & Release Notes
- Summarize user-facing changes since the previous release/tag (from `git log <prev-tag>..HEAD` or recent session changes) into concise Markdown release notes:
  - **What's New / Fixed** (bulleted list)
  - **What's Included** (`Khutaf.exe`, `runtime/khutaf-hook64.dll`)
  - **Notes** (portable install, optional in-app `TMP_Font_AssetBundles` download, Windows 10/11 x64 + WebView2 requirement)
- If `gh` CLI is installed and authenticated, offer or run `gh release create v<version> dist/Khutaf-v<version>-win64.zip dist/SHA256SUMS.txt ...`; otherwise provide the output zip & checksum paths and copy-pasteable release notes.
