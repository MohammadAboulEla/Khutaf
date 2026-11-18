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

## 2. Run Verification Checks
- Run Go vet & tests, and verify the frontend builds cleanly:
  ```powershell
  go vet ./...
  go test ./...
  npm --prefix frontend run build
  ```

## 3. Build Native Hook DLL & Wails Binary
- Rebuild `runtime/khutaf-hook64.dll`:
  ```powershell
  powershell -ExecutionPolicy Bypass -File scripts/build-native.ps1
  ```
- Build `build/bin/Khutaf.exe` with `AppVersion` injected:
  ```powershell
  wails build -ldflags "-X main.AppVersion=<version>"
  ```

## 4. Package Release Zip in `dist/`
- Stage only `Khutaf.exe` and `runtime/khutaf-hook64.dll` (never include `runtime/TMP_Font_AssetBundles/` — users download that in-app from the `unity-fonts` release).
- Zip with `.NET ZipFile` (avoid `Compress-Archive` progress-bar errors in non-interactive shells):
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
  Get-Item $zip | Select-Object Name, Length
  ```

## 5. Generate Changelog & Release Notes
- Summarize user-facing changes since the previous release/tag (from `git log <prev-tag>..HEAD` or recent session changes) into concise Markdown release notes:
  - **What's New / Fixed** (bulleted list)
  - **What's Included** (`Khutaf.exe`, `runtime/khutaf-hook64.dll`)
  - **Notes** (portable install, optional in-app `TMP_Font_AssetBundles` download, Windows 10/11 x64 + WebView2 requirement)
- If `gh` CLI is installed and authenticated, offer or run `gh release create v<version> dist/Khutaf-v<version>-win64.zip ...`; otherwise provide the output zip path and copy-pasteable release notes.
