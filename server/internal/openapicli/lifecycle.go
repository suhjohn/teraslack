package openapicli

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

var Version = "dev"

const defaultCLIManifestBaseURL = "https://downloads.teraslack.ai/teraslack/cli"
const defaultMCPManifestBaseURL = "https://downloads.teraslack.ai/teraslack/mcp"

type releaseManifest struct {
	Version   string                     `json:"version"`
	Artifacts map[string]releaseArtifact `json:"artifacts"`
}

type releaseArtifact struct {
	URL         string `json:"url"`
	SHA256      string `json:"sha256"`
	ArchiveName string `json:"archive_name"`
	BinaryName  string `json:"binary_name"`
}

func isLifecycleCommand(name string) bool {
	switch strings.TrimSpace(name) {
	case "version", "update", "uninstall", "signout":
		return true
	default:
		return false
	}
}

func (c *CLI) runLifecycle(ctx context.Context, name string, args []string, output string, stdout io.Writer, stderr io.Writer) int {
	switch strings.TrimSpace(name) {
	case "version":
		return c.runVersion(args, output, stdout, stderr)
	case "update":
		return c.runUpdate(ctx, args, stdout, stderr)
	case "uninstall":
		return c.runUninstall(args, stdout, stderr)
	case "signout":
		return c.runSignout(args, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown command %q\n", name)
		return 2
	}
}

func (c *CLI) printLifecycleHelp(name string, w io.Writer) {
	switch name {
	case "version":
		fmt.Fprintln(w, "Usage:\n  teraslack version")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Show the installed Teraslack CLI version.")
	case "update":
		fmt.Fprintln(w, "Usage:\n  teraslack update")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Download the latest published CLI and MCP releases for this platform and replace the installed binaries.")
	case "uninstall":
		fmt.Fprintln(w, "Usage:\n  teraslack uninstall [--keep-config]")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Remove the installed CLI and MCP binaries and clean up managed Codex/Claude integrations. By default this also deletes saved CLI config files under TERASLACK_CONFIG_DIR (defaults to ~/.teraslack).")
	case "signout":
		fmt.Fprintln(w, "Usage:\n  teraslack signout")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Remove the saved session token while keeping the base URL and API key config.")
	}
}

func (c *CLI) runVersion(args []string, output string, stdout io.Writer, stderr io.Writer) int {
	fs := flag.NewFlagSet("version", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		c.printLifecycleHelp("version", stderr)
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if len(fs.Args()) != 0 {
		fs.Usage()
		return 2
	}

	payload := map[string]any{
		"name":    "teraslack",
		"version": strings.TrimSpace(Version),
	}
	if output == "json" {
		if err := writeOutput(stdout, payload, output); err != nil {
			fmt.Fprintf(stderr, "write output: %v\n", err)
			return 1
		}
		return 0
	}
	fmt.Fprintf(stdout, "teraslack %s\n", strings.TrimSpace(Version))
	return 0
}

func (c *CLI) runUpdate(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) int {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		c.printLifecycleHelp("update", stderr)
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if len(fs.Args()) != 0 {
		fs.Usage()
		return 2
	}

	cliManifest, cliArtifact, mcpManifest, mcpArtifact, platform, err := resolveLatestToolArtifacts(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "resolve release: %v\n", err)
		return 1
	}
	if strings.TrimSpace(cliManifest.Version) != strings.TrimSpace(mcpManifest.Version) {
		fmt.Fprintf(stderr, "release manifests are out of sync: cli=%s mcp=%s\n", strings.TrimSpace(cliManifest.Version), strings.TrimSpace(mcpManifest.Version))
		return 1
	}

	mcpPath, err := installedMCPBinaryPathForPlatform(platform)
	if err != nil {
		fmt.Fprintf(stderr, "resolve mcp executable: %v\n", err)
		return 1
	}
	if strings.TrimSpace(Version) == strings.TrimSpace(cliManifest.Version) && Version != "dev" && fileExists(mcpPath) {
		fmt.Fprintf(stdout, "teraslack %s is already installed\n", cliManifest.Version)
		return 0
	}

	exePath, err := currentExecutablePath()
	if err != nil {
		fmt.Fprintf(stderr, "resolve executable: %v\n", err)
		return 1
	}

	tmpDir, err := os.MkdirTemp("", "teraslack-update-*")
	if err != nil {
		fmt.Fprintf(stderr, "create temp dir: %v\n", err)
		return 1
	}
	defer os.RemoveAll(tmpDir)

	extractedCLIPath, err := downloadAndExtractReleaseBinary(ctx, cliArtifact, tmpDir, platform, defaultBinaryNameForPlatform(platform))
	if err != nil {
		fmt.Fprintf(stderr, "download CLI release: %v\n", err)
		return 1
	}
	extractedMCPPath, err := downloadAndExtractReleaseBinary(ctx, mcpArtifact, tmpDir, platform, defaultMCPBinaryNameForPlatform(platform))
	if err != nil {
		fmt.Fprintf(stderr, "download MCP release: %v\n", err)
		return 1
	}

	if runtime.GOOS == "windows" {
		if err := scheduleWindowsReplace(extractedCLIPath, exePath); err != nil {
			fmt.Fprintf(stderr, "schedule update: %v\n", err)
			return 1
		}
		if err := installStandaloneBinary(extractedMCPPath, mcpPath); err != nil {
			fmt.Fprintf(stderr, "install MCP update: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "scheduled update to %s; reopen teraslack after this process exits\n", cliManifest.Version)
		return 0
	}

	if err := replaceExecutable(extractedCLIPath, exePath); err != nil {
		fmt.Fprintf(stderr, "install update: %v\n", err)
		return 1
	}
	if err := installStandaloneBinary(extractedMCPPath, mcpPath); err != nil {
		fmt.Fprintf(stderr, "install MCP update: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "updated teraslack and teraslack-mcp to %s\n", cliManifest.Version)
	return 0
}

func (c *CLI) runUninstall(args []string, stdout io.Writer, stderr io.Writer) int {
	var keepConfig bool

	fs := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.BoolVar(&keepConfig, "keep-config", false, "Keep saved CLI config files under TERASLACK_CONFIG_DIR.")
	fs.Usage = func() {
		c.printLifecycleHelp("uninstall", stderr)
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if len(fs.Args()) != 0 {
		fs.Usage()
		return 2
	}

	if err := uninstallCodexIntegration(); err != nil {
		fmt.Fprintf(stderr, "remove Codex integration: %v\n", err)
		return 1
	}
	if err := uninstallClaudeIntegration(); err != nil {
		fmt.Fprintf(stderr, "remove Claude integration: %v\n", err)
		return 1
	}

	exePath, err := currentExecutablePath()
	if err != nil {
		fmt.Fprintf(stderr, "resolve executable: %v\n", err)
		return 1
	}

	installRoot, err := installRootPath()
	if err != nil {
		fmt.Fprintf(stderr, "resolve install root: %v\n", err)
		return 1
	}
	binDir := filepath.Join(installRoot, "bin")
	mcpInstallRoot, err := mcpInstallRootPath()
	if err != nil {
		fmt.Fprintf(stderr, "resolve mcp install root: %v\n", err)
		return 1
	}
	mcpBinDir, err := mcpBinDirPath()
	if err != nil {
		fmt.Fprintf(stderr, "resolve mcp bin dir: %v\n", err)
		return 1
	}
	mcpPath, err := installedMCPBinaryPathForPlatform(runtimePlatformLabel())
	if err != nil && runtime.GOOS != "windows" {
		fmt.Fprintf(stderr, "resolve mcp binary path: %v\n", err)
		return 1
	}
	configPath, err := configFilePath()
	if err != nil {
		fmt.Fprintf(stderr, "resolve config path: %v\n", err)
		return 1
	}
	linksPath, err := linksFilePath()
	if err != nil {
		fmt.Fprintf(stderr, "resolve links path: %v\n", err)
		return 1
	}
	configDir, err := configDirPath()
	if err != nil {
		fmt.Fprintf(stderr, "resolve config dir: %v\n", err)
		return 1
	}

	if runtime.GOOS == "windows" {
		if err := scheduleWindowsUninstall(exePath, mcpPath, configPath, linksPath, configDir, binDir, mcpBinDir, installRoot, mcpInstallRoot, keepConfig); err != nil {
			fmt.Fprintf(stderr, "schedule uninstall: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, "scheduled teraslack uninstall; reopen your shell after this process exits")
		return 0
	}

	if !keepConfig {
		removeConfigArtifacts(configPath, linksPath, configDir)
	}
	if err := os.Remove(exePath); err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(stderr, "remove executable: %v\n", err)
		return 1
	}
	if err := os.Remove(mcpPath); err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(stderr, "remove mcp executable: %v\n", err)
		return 1
	}
	removeUnixInstallPathEntry(binDir)
	removeDirIfEmpty(mcpBinDir)
	removeDirIfEmpty(mcpInstallRoot)
	removeDirIfEmpty(binDir)
	removeDirIfEmpty(installRoot)
	fmt.Fprintln(stdout, "uninstalled teraslack")
	return 0
}

func (c *CLI) runSignout(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := flag.NewFlagSet("signout", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		c.printLifecycleHelp("signout", stderr)
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if len(fs.Args()) != 0 {
		fs.Usage()
		return 2
	}

	if err := clearSessionFromFileConfig(); err != nil {
		fmt.Fprintf(stderr, "clear session: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, "signed out of teraslack")
	return 0
}

func resolveLatestToolArtifacts(ctx context.Context) (*releaseManifest, releaseArtifact, *releaseManifest, releaseArtifact, string, error) {
	platform, err := detectRuntimePlatform()
	if err != nil {
		return nil, releaseArtifact{}, nil, releaseArtifact{}, "", err
	}
	cliManifest, err := fetchManifest(ctx, currentCLIManifestURL())
	if err != nil {
		return nil, releaseArtifact{}, nil, releaseArtifact{}, "", err
	}
	mcpManifest, err := fetchManifest(ctx, currentMCPManifestURL())
	if err != nil {
		return nil, releaseArtifact{}, nil, releaseArtifact{}, "", err
	}
	cliArtifact, ok := cliManifest.Artifacts[platform]
	if !ok {
		return nil, releaseArtifact{}, nil, releaseArtifact{}, "", fmt.Errorf("no CLI release artifact for %s", platform)
	}
	if strings.TrimSpace(cliArtifact.URL) == "" {
		return nil, releaseArtifact{}, nil, releaseArtifact{}, "", fmt.Errorf("CLI manifest artifact for %s is missing a URL", platform)
	}
	if strings.TrimSpace(cliArtifact.SHA256) == "" {
		return nil, releaseArtifact{}, nil, releaseArtifact{}, "", fmt.Errorf("CLI manifest artifact for %s is missing a sha256", platform)
	}
	mcpArtifact, ok := mcpManifest.Artifacts[platform]
	if !ok {
		return nil, releaseArtifact{}, nil, releaseArtifact{}, "", fmt.Errorf("no MCP release artifact for %s", platform)
	}
	if strings.TrimSpace(mcpArtifact.URL) == "" {
		return nil, releaseArtifact{}, nil, releaseArtifact{}, "", fmt.Errorf("MCP manifest artifact for %s is missing a URL", platform)
	}
	if strings.TrimSpace(mcpArtifact.SHA256) == "" {
		return nil, releaseArtifact{}, nil, releaseArtifact{}, "", fmt.Errorf("MCP manifest artifact for %s is missing a sha256", platform)
	}
	return cliManifest, cliArtifact, mcpManifest, mcpArtifact, platform, nil
}

func fetchManifest(ctx context.Context, manifestURL string) (*releaseManifest, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, manifestURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("manifest request returned %s", resp.Status)
	}

	var manifest releaseManifest
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return nil, err
	}
	return &manifest, nil
}

func currentCLIManifestURL() string {
	if value := strings.TrimSpace(os.Getenv("TERASLACK_CLI_MANIFEST_URL")); value != "" {
		return value
	}
	baseURL := strings.TrimSpace(os.Getenv("TERASLACK_CLI_DOWNLOAD_BASE_URL"))
	if baseURL == "" {
		baseURL = strings.TrimSpace(os.Getenv("TERASLACK_DOWNLOAD_BASE_URL"))
	}
	if baseURL == "" {
		baseURL = defaultCLIManifestBaseURL
	}
	return strings.TrimRight(baseURL, "/") + "/latest.json"
}

func currentMCPManifestURL() string {
	if value := strings.TrimSpace(os.Getenv("TERASLACK_MCP_MANIFEST_URL")); value != "" {
		return value
	}
	baseURL := strings.TrimSpace(os.Getenv("TERASLACK_MCP_DOWNLOAD_BASE_URL"))
	if baseURL == "" {
		baseURL = defaultMCPManifestBaseURL
	}
	return strings.TrimRight(baseURL, "/") + "/latest.json"
}

func detectRuntimePlatform() (string, error) {
	switch runtime.GOOS {
	case "darwin":
		switch runtime.GOARCH {
		case "arm64":
			return "darwin-arm64", nil
		case "amd64":
			return "darwin-amd64", nil
		}
	case "linux":
		switch runtime.GOARCH {
		case "amd64":
			return "linux-amd64", nil
		case "arm64":
			return "linux-arm64", nil
		}
	case "windows":
		switch runtime.GOARCH {
		case "amd64":
			return "windows-amd64", nil
		case "arm64":
			return "windows-arm64", nil
		}
	}
	return "", fmt.Errorf("unsupported platform: %s/%s", runtime.GOOS, runtime.GOARCH)
}

func defaultBinaryNameForPlatform(platform string) string {
	if strings.HasPrefix(platform, "windows-") {
		return "teraslack.exe"
	}
	return "teraslack"
}

func defaultMCPBinaryNameForPlatform(platform string) string {
	if strings.HasPrefix(platform, "windows-") {
		return "teraslack-mcp.exe"
	}
	return "teraslack-mcp"
}

func currentExecutablePath() (string, error) {
	path, err := os.Executable()
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		return resolved, nil
	}
	if os.IsNotExist(err) {
		return path, nil
	}
	return "", err
}

func installRootPath() (string, error) {
	if value := strings.TrimSpace(os.Getenv("TERASLACK_INSTALL_ROOT")); value != "" {
		return value, nil
	}
	if exePath, err := currentExecutablePath(); err == nil {
		binDir := filepath.Dir(exePath)
		if filepath.Base(binDir) == "bin" {
			return filepath.Dir(binDir), nil
		}
	}
	return defaultConfigRootPath()
}

func mcpInstallRootPath() (string, error) {
	root, err := installRootPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "mcp"), nil
}

func mcpBinDirPath() (string, error) {
	root, err := mcpInstallRootPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "bin"), nil
}

func installedMCPBinaryPath() (string, error) {
	platform, err := detectRuntimePlatform()
	if err != nil {
		return "", err
	}
	return installedMCPBinaryPathForPlatform(platform)
}

func installedMCPBinaryPathForPlatform(platform string) (string, error) {
	dir, err := mcpBinDirPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, defaultMCPBinaryNameForPlatform(platform)), nil
}

func runtimePlatformLabel() string {
	platform, err := detectRuntimePlatform()
	if err != nil {
		return ""
	}
	return platform
}

func configDirPath() (string, error) {
	if value := strings.TrimSpace(os.Getenv("TERASLACK_CONFIG_DIR")); value != "" {
		return value, nil
	}
	return defaultConfigRootPath()
}

func defaultConfigRootPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(homeDir, ".teraslack"), nil
}

func configFilePath() (string, error) {
	if value := strings.TrimSpace(os.Getenv("TERASLACK_CONFIG_FILE")); value != "" {
		return value, nil
	}
	root, err := configDirPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "config.json"), nil
}

func linksFilePath() (string, error) {
	root, err := configDirPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "links.json"), nil
}

func downloadFile(ctx context.Context, sourceURL string, destination string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download request returned %s", resp.Status)
	}

	file, err := os.Create(destination)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = io.Copy(file, resp.Body)
	return err
}

func verifyFileSHA256(path string, expected string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return err
	}
	actual := hex.EncodeToString(hash.Sum(nil))
	if !strings.EqualFold(strings.TrimSpace(actual), strings.TrimSpace(expected)) {
		return fmt.Errorf("sha256 mismatch")
	}
	return nil
}

func extractBinaryFromArchive(archivePath string, tmpDir string, binaryName string) (string, error) {
	if strings.HasSuffix(strings.ToLower(archivePath), ".zip") {
		return extractBinaryFromZip(archivePath, tmpDir, binaryName)
	}
	return extractBinaryFromTarGz(archivePath, tmpDir, binaryName)
}

func extractBinaryFromZip(archivePath string, tmpDir string, binaryName string) (string, error) {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return "", err
	}
	defer reader.Close()

	for _, file := range reader.File {
		if filepath.Base(file.Name) != binaryName {
			continue
		}
		in, err := file.Open()
		if err != nil {
			return "", err
		}
		defer in.Close()

		destination := filepath.Join(tmpDir, binaryName)
		if err := writeExecutableFile(destination, in, 0o755); err != nil {
			return "", err
		}
		return destination, nil
	}
	return "", fmt.Errorf("archive did not contain %s", binaryName)
}

func extractBinaryFromTarGz(archivePath string, tmpDir string, binaryName string) (string, error) {
	file, err := os.Open(archivePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return "", err
	}
	defer gzipReader.Close()

	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", err
		}
		if header == nil || filepath.Base(header.Name) != binaryName {
			continue
		}
		destination := filepath.Join(tmpDir, binaryName)
		mode := os.FileMode(0o755)
		if header.Mode != 0 {
			mode = os.FileMode(header.Mode)
		}
		if err := writeExecutableFile(destination, tarReader, mode); err != nil {
			return "", err
		}
		return destination, nil
	}
	return "", fmt.Errorf("archive did not contain %s", binaryName)
}

func writeExecutableFile(path string, reader io.Reader, mode os.FileMode) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := io.Copy(file, reader); err != nil {
		return err
	}
	if runtime.GOOS != "windows" {
		return os.Chmod(path, mode)
	}
	return nil
}

func replaceExecutable(sourcePath string, targetPath string) error {
	stagedPath := targetPath + ".new"
	input, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer input.Close()
	if err := writeExecutableFile(stagedPath, input, 0o755); err != nil {
		return err
	}
	return os.Rename(stagedPath, targetPath)
}

func installStandaloneBinary(sourcePath string, targetPath string) error {
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return err
	}
	input, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer input.Close()
	stagedPath := targetPath + ".new"
	if err := writeExecutableFile(stagedPath, input, 0o755); err != nil {
		return err
	}
	return os.Rename(stagedPath, targetPath)
}

func scheduleWindowsReplace(sourcePath string, targetPath string) error {
	stagedPath := targetPath + ".new"
	input, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer input.Close()
	if err := writeExecutableFile(stagedPath, input, 0o755); err != nil {
		return err
	}

	script := fmt.Sprintf(`$src=%q; $dst=%q; for ($i=0; $i -lt 20; $i++) { try { Remove-Item -LiteralPath $dst -Force -ErrorAction SilentlyContinue; Move-Item -LiteralPath $src -Destination $dst -Force; exit 0 } catch { Start-Sleep -Milliseconds 500 } } exit 1`, stagedPath, targetPath)
	return startWindowsPowerShell(script)
}

func scheduleWindowsUninstall(exePath string, mcpPath string, configPath string, linksPath string, configDir string, binDir string, mcpBinDir string, installRoot string, mcpInstallRoot string, keepConfig bool) error {
	configRemoval := ""
	if !keepConfig {
		configRemoval = fmt.Sprintf(`foreach ($path in @(%q,%q)) { if ($path -and (Test-Path -LiteralPath $path)) { Remove-Item -LiteralPath $path -Force -ErrorAction SilentlyContinue } }; if (Test-Path -LiteralPath %q) { $children=Get-ChildItem -LiteralPath %q -Force -ErrorAction SilentlyContinue; if (-not $children) { Remove-Item -LiteralPath %q -Recurse -Force -ErrorAction SilentlyContinue } }`, configPath, linksPath, configDir, configDir, configDir)
	}
	script := fmt.Sprintf(`$binDir=%q; $mcpBinDir=%q; $installRoot=%q; $mcpInstallRoot=%q; %s; $currentUserPath=[Environment]::GetEnvironmentVariable("Path","User"); if ($currentUserPath) { $entries=$currentUserPath.Split(";",[System.StringSplitOptions]::RemoveEmptyEntries) | Where-Object { $_.TrimEnd("\") -ine $binDir.TrimEnd("\") }; [Environment]::SetEnvironmentVariable("Path", ($entries -join ";"), "User") }; foreach ($path in @(%q,%q)) { for ($i=0; $i -lt 20; $i++) { try { if ($path -and (Test-Path -LiteralPath $path)) { Remove-Item -LiteralPath $path -Force -ErrorAction SilentlyContinue }; break } catch { Start-Sleep -Milliseconds 500 } } }; foreach ($path in @($mcpBinDir,$binDir,$mcpInstallRoot,$installRoot)) { if (Test-Path -LiteralPath $path) { $children=Get-ChildItem -LiteralPath $path -Force -ErrorAction SilentlyContinue; if (-not $children) { Remove-Item -LiteralPath $path -Recurse -Force -ErrorAction SilentlyContinue } } }`, binDir, mcpBinDir, installRoot, mcpInstallRoot, configRemoval, mcpPath, exePath)
	return startWindowsPowerShell(script)
}

func startWindowsPowerShell(script string) error {
	commandName := "powershell"
	if _, err := exec.LookPath(commandName); err != nil {
		commandName = "pwsh"
		if _, err := exec.LookPath(commandName); err != nil {
			return fmt.Errorf("missing PowerShell")
		}
	}
	cmd := exec.Command(commandName, "-NoProfile", "-NonInteractive", "-Command", script)
	return cmd.Start()
}

func removeUnixInstallPathEntry(binDir string) {
	profiles := []string{}
	if zdotdir := strings.TrimSpace(os.Getenv("ZDOTDIR")); zdotdir != "" {
		profiles = append(profiles, filepath.Join(zdotdir, ".zprofile"))
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return
	}
	profiles = append(profiles,
		filepath.Join(homeDir, ".zprofile"),
		filepath.Join(homeDir, ".bash_profile"),
		filepath.Join(homeDir, ".profile"),
	)
	for _, path := range profiles {
		_ = rewriteShellProfile(path, binDir)
	}
}

func rewriteShellProfile(path string, binDir string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	updated := stripInstallerPathBlock(string(data), binDir)
	if updated == string(data) {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	return os.WriteFile(path, []byte(updated), info.Mode())
}

func stripInstallerPathBlock(content string, binDir string) string {
	lines := strings.Split(content, "\n")
	targetLine := fmt.Sprintf(`export PATH="%s:$PATH"`, binDir)
	out := make([]string, 0, len(lines))

	for _, line := range lines {
		if line == targetLine {
			if len(out) > 0 && out[len(out)-1] == "# Added by Teraslack installer" {
				out = out[:len(out)-1]
				if len(out) > 0 && out[len(out)-1] == "" {
					out = out[:len(out)-1]
				}
			}
			continue
		}
		out = append(out, line)
	}

	updated := strings.Join(out, "\n")
	if strings.HasSuffix(content, "\n") && !strings.HasSuffix(updated, "\n") {
		updated += "\n"
	}
	return updated
}

func removeDirIfEmpty(path string) {
	entries, err := os.ReadDir(path)
	if err != nil || len(entries) != 0 {
		return
	}
	_ = os.Remove(path)
}

func removeConfigArtifacts(configPath string, linksPath string, configDir string) {
	_ = os.Remove(configPath)
	_ = os.Remove(linksPath)
	removeDirIfEmpty(configDir)
}

func downloadAndExtractReleaseBinary(ctx context.Context, artifact releaseArtifact, tmpDir string, platform string, fallbackBinaryName string) (string, error) {
	archivePath := filepath.Join(tmpDir, filepath.Base(strings.TrimSpace(artifact.URL)))
	if err := downloadFile(ctx, strings.TrimSpace(artifact.URL), archivePath); err != nil {
		return "", err
	}
	if err := verifyFileSHA256(archivePath, artifact.SHA256); err != nil {
		return "", err
	}

	binaryName := strings.TrimSpace(artifact.BinaryName)
	if binaryName == "" {
		binaryName = fallbackBinaryName
	}
	return extractBinaryFromArchive(archivePath, tmpDir, binaryName)
}

func fileExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
