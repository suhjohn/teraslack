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
		fmt.Fprintln(w, "Print the installed Teraslack CLI version.")
	case "update":
		fmt.Fprintln(w, "Usage:\n  teraslack update")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Download the latest CLI release from the configured manifest and replace the current binary.")
	case "uninstall":
		fmt.Fprintln(w, "Usage:\n  teraslack uninstall [--keep-config]")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Remove the installed CLI binary. By default this also deletes ~/.teraslack/config.json.")
	case "signout":
		fmt.Fprintln(w, "Usage:\n  teraslack signout")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Remove the stored session token while keeping base URL and API key config.")
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

	manifest, artifact, platform, err := resolveLatestArtifact(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "resolve release: %v\n", err)
		return 1
	}
	if strings.TrimSpace(Version) == strings.TrimSpace(manifest.Version) && Version != "dev" {
		fmt.Fprintf(stdout, "teraslack %s is already installed\n", manifest.Version)
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

	archivePath := filepath.Join(tmpDir, filepath.Base(strings.TrimSpace(artifact.URL)))
	if err := downloadFile(ctx, strings.TrimSpace(artifact.URL), archivePath); err != nil {
		fmt.Fprintf(stderr, "download release: %v\n", err)
		return 1
	}
	if err := verifyFileSHA256(archivePath, artifact.SHA256); err != nil {
		fmt.Fprintf(stderr, "verify release: %v\n", err)
		return 1
	}

	binaryName := strings.TrimSpace(artifact.BinaryName)
	if binaryName == "" {
		binaryName = defaultBinaryNameForPlatform(platform)
	}
	extractedPath, err := extractBinaryFromArchive(archivePath, tmpDir, binaryName)
	if err != nil {
		fmt.Fprintf(stderr, "extract release: %v\n", err)
		return 1
	}

	if runtime.GOOS == "windows" {
		if err := scheduleWindowsReplace(extractedPath, exePath); err != nil {
			fmt.Fprintf(stderr, "schedule update: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "scheduled update to %s; reopen teraslack after this process exits\n", manifest.Version)
		return 0
	}

	if err := replaceExecutable(extractedPath, exePath); err != nil {
		fmt.Fprintf(stderr, "install update: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "updated teraslack to %s\n", manifest.Version)
	return 0
}

func (c *CLI) runUninstall(args []string, stdout io.Writer, stderr io.Writer) int {
	var keepConfig bool

	fs := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.BoolVar(&keepConfig, "keep-config", false, "Keep ~/.teraslack/config.json.")
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
	configPath, err := configFilePath()
	if err != nil {
		fmt.Fprintf(stderr, "resolve config path: %v\n", err)
		return 1
	}

	if runtime.GOOS == "windows" {
		if err := scheduleWindowsUninstall(exePath, configPath, binDir, installRoot, keepConfig); err != nil {
			fmt.Fprintf(stderr, "schedule uninstall: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, "scheduled teraslack uninstall; reopen your shell after this process exits")
		return 0
	}

	if !keepConfig {
		_ = os.Remove(configPath)
	}
	if err := os.Remove(exePath); err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(stderr, "remove executable: %v\n", err)
		return 1
	}
	removeUnixInstallPathEntry(binDir)
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

func resolveLatestArtifact(ctx context.Context) (*releaseManifest, releaseArtifact, string, error) {
	platform, err := detectRuntimePlatform()
	if err != nil {
		return nil, releaseArtifact{}, "", err
	}
	manifest, err := fetchManifest(ctx, currentManifestURL())
	if err != nil {
		return nil, releaseArtifact{}, "", err
	}
	artifact, ok := manifest.Artifacts[platform]
	if !ok {
		return nil, releaseArtifact{}, "", fmt.Errorf("no release artifact for %s", platform)
	}
	if strings.TrimSpace(artifact.URL) == "" {
		return nil, releaseArtifact{}, "", fmt.Errorf("manifest artifact for %s is missing a URL", platform)
	}
	if strings.TrimSpace(artifact.SHA256) == "" {
		return nil, releaseArtifact{}, "", fmt.Errorf("manifest artifact for %s is missing a sha256", platform)
	}
	return manifest, artifact, platform, nil
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

func currentManifestURL() string {
	if value := strings.TrimSpace(os.Getenv("TERASLACK_CLI_MANIFEST_URL")); value != "" {
		return value
	}
	baseURL := strings.TrimSpace(os.Getenv("TERASLACK_DOWNLOAD_BASE_URL"))
	if baseURL == "" {
		baseURL = defaultCLIManifestBaseURL
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
	root, err := installRootPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "config.json"), nil
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

func scheduleWindowsUninstall(exePath string, configPath string, binDir string, installRoot string, keepConfig bool) error {
	configRemoval := ""
	if !keepConfig {
		configRemoval = fmt.Sprintf(`if (Test-Path -LiteralPath %q) { Remove-Item -LiteralPath %q -Force -ErrorAction SilentlyContinue }`, configPath, configPath)
	}
	script := fmt.Sprintf(`$binDir=%q; $installRoot=%q; %s; $currentUserPath=[Environment]::GetEnvironmentVariable("Path","User"); if ($currentUserPath) { $entries=$currentUserPath.Split(";",[System.StringSplitOptions]::RemoveEmptyEntries) | Where-Object { $_.TrimEnd("\") -ine $binDir.TrimEnd("\") }; [Environment]::SetEnvironmentVariable("Path", ($entries -join ";"), "User") }; for ($i=0; $i -lt 20; $i++) { try { if (Test-Path -LiteralPath %q) { Remove-Item -LiteralPath %q -Force -ErrorAction SilentlyContinue }; break } catch { Start-Sleep -Milliseconds 500 } }; if (Test-Path -LiteralPath $binDir) { Remove-Item -LiteralPath $binDir -Recurse -Force -ErrorAction SilentlyContinue }; if (Test-Path -LiteralPath $installRoot) { $children=Get-ChildItem -LiteralPath $installRoot -Force -ErrorAction SilentlyContinue; if (-not $children) { Remove-Item -LiteralPath $installRoot -Recurse -Force -ErrorAction SilentlyContinue } }`, binDir, installRoot, configRemoval, exePath, exePath)
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
