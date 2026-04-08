package openapicli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const directoryLinksFileVersion = 1

type directoryLink struct {
	Path           string `json:"path"`
	ConversationID string `json:"conversation_id"`
	LinkedAt       string `json:"linked_at"`
}

type directoryLinksFile struct {
	Version int             `json:"version"`
	Links   []directoryLink `json:"links"`
}

type linkCommandResponse struct {
	CurrentPath    string `json:"current_path"`
	LinkedPath     string `json:"linked_path"`
	ConversationID string `json:"conversation_id"`
	LinkedAt       string `json:"linked_at"`
}

func (c *CLI) runLink(args []string, output string, stdout io.Writer, stderr io.Writer) int {
	var conversationID string

	fs := flag.NewFlagSet("link", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&conversationID, "conversation", "", "Conversation ID to associate with the current directory.")
	fs.Usage = func() {
		c.printLinkHelp(stderr)
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if len(fs.Args()) != 0 {
		fs.Usage()
		return 2
	}

	currentPath, err := canonicalWorkingDirectory()
	if err != nil {
		fmt.Fprintf(stderr, "resolve current directory: %v\n", err)
		return 1
	}

	if strings.TrimSpace(conversationID) != "" {
		link, err := upsertDirectoryLink(currentPath, conversationID, time.Now().UTC())
		if err != nil {
			fmt.Fprintf(stderr, "save link: %v\n", err)
			return 1
		}
		response := linkCommandResponse{
			CurrentPath:    currentPath,
			LinkedPath:     link.Path,
			ConversationID: link.ConversationID,
			LinkedAt:       link.LinkedAt,
		}
		if output == "json" {
			if err := writeOutput(stdout, response, output); err != nil {
				fmt.Fprintf(stderr, "write output: %v\n", err)
				return 1
			}
			return 0
		}
		fmt.Fprintf(stdout, "linked %s to conversation %s\n", response.LinkedPath, response.ConversationID)
		return 0
	}

	link, ok, err := resolveDirectoryLink(currentPath)
	if err != nil {
		fmt.Fprintf(stderr, "load link: %v\n", err)
		return 1
	}
	if !ok {
		fmt.Fprintf(stderr, "no linked conversation for %s\n", currentPath)
		return 1
	}
	response := linkCommandResponse{
		CurrentPath:    currentPath,
		LinkedPath:     link.Path,
		ConversationID: link.ConversationID,
		LinkedAt:       link.LinkedAt,
	}
	if output == "json" {
		if err := writeOutput(stdout, response, output); err != nil {
			fmt.Fprintf(stderr, "write output: %v\n", err)
			return 1
		}
		return 0
	}
	if response.CurrentPath == response.LinkedPath {
		fmt.Fprintf(stdout, "%s -> %s\n", response.LinkedPath, response.ConversationID)
		return 0
	}
	fmt.Fprintf(stdout, "%s -> %s (via %s)\n", response.CurrentPath, response.ConversationID, response.LinkedPath)
	return 0
}

func (c *CLI) printLinkHelp(w io.Writer) {
	fmt.Fprintln(w, "Usage:\n  teraslack link [--conversation <conversation-id>]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Without --conversation, show the nearest linked conversation for the current directory.")
	fmt.Fprintln(w, "With --conversation, save or update the current directory link in TERASLACK_CONFIG_DIR (defaults to ~/.teraslack).")
}

func loadDirectoryLinks() (directoryLinksFile, error) {
	path, err := linksFilePath()
	if err != nil {
		return directoryLinksFile{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return directoryLinksFile{Version: directoryLinksFileVersion}, nil
		}
		return directoryLinksFile{}, err
	}
	var links directoryLinksFile
	if err := json.Unmarshal(data, &links); err != nil {
		return directoryLinksFile{}, err
	}
	if links.Version == 0 {
		links.Version = directoryLinksFileVersion
	}
	return links, nil
}

func saveDirectoryLinks(links directoryLinksFile) error {
	path, err := linksFilePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	links.Version = directoryLinksFileVersion
	sort.Slice(links.Links, func(i, j int) bool {
		return links.Links[i].Path < links.Links[j].Path
	})
	data, err := json.MarshalIndent(links, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o600)
}

func upsertDirectoryLink(path string, conversationID string, linkedAt time.Time) (directoryLink, error) {
	canonicalPath, err := canonicalLinkPath(path)
	if err != nil {
		return directoryLink{}, err
	}
	conversationID = strings.TrimSpace(conversationID)
	if conversationID == "" {
		return directoryLink{}, fmt.Errorf("conversation ID must not be empty")
	}
	links, err := loadDirectoryLinks()
	if err != nil {
		return directoryLink{}, err
	}
	entry := directoryLink{
		Path:           canonicalPath,
		ConversationID: conversationID,
		LinkedAt:       linkedAt.UTC().Format(time.RFC3339),
	}
	for i := range links.Links {
		if links.Links[i].Path == canonicalPath {
			links.Links[i] = entry
			if err := saveDirectoryLinks(links); err != nil {
				return directoryLink{}, err
			}
			return entry, nil
		}
	}
	links.Links = append(links.Links, entry)
	if err := saveDirectoryLinks(links); err != nil {
		return directoryLink{}, err
	}
	return entry, nil
}

func resolveDirectoryLink(path string) (directoryLink, bool, error) {
	canonicalPath, err := canonicalLinkPath(path)
	if err != nil {
		return directoryLink{}, false, err
	}
	links, err := loadDirectoryLinks()
	if err != nil {
		return directoryLink{}, false, err
	}
	byPath := make(map[string]directoryLink, len(links.Links))
	for _, link := range links.Links {
		byPath[link.Path] = link
	}
	current := canonicalPath
	for {
		if link, ok := byPath[current]; ok {
			return link, true, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return directoryLink{}, false, nil
		}
		current = parent
	}
}

func canonicalWorkingDirectory() (string, error) {
	path, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return canonicalLinkPath(path)
}

func canonicalLinkPath(path string) (string, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return "", fmt.Errorf("path must not be empty")
	}
	absolutePath, err := filepath.Abs(trimmed)
	if err != nil {
		return "", err
	}
	cleanPath := filepath.Clean(absolutePath)
	resolvedPath, err := filepath.EvalSymlinks(cleanPath)
	if err == nil {
		return resolvedPath, nil
	}
	if os.IsNotExist(err) {
		return cleanPath, nil
	}
	return "", err
}
