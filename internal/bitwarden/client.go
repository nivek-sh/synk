package bitwarden

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

type CLI struct {
	Path    string
	Session string
}

type Status struct {
	Status string `json:"status"`
	UserID string `json:"userId"`
}

type VaultLockedError struct {
	Status string
}

func (e VaultLockedError) Error() string {
	switch e.Status {
	case "unauthenticated":
		return "Bitwarden CLI is not logged in; run `bw login` first"
	case "locked":
		return "Bitwarden vault is locked; unlock with `bw unlock` and try again"
	default:
		return fmt.Sprintf("Bitwarden vault is not unlocked (status: %s)", e.Status)
	}
}

func NewCLI(path string) CLI {
	if strings.TrimSpace(path) == "" {
		path = "bw"
	}
	return CLI{Path: path}
}

func (c CLI) Available() bool {
	_, err := exec.LookPath(c.Path)
	return err == nil
}

func (c CLI) Sync(ctx context.Context) error {
	_, err := c.run(ctx, "sync")
	return err
}

func (c CLI) Status(ctx context.Context) (Status, error) {
	out, err := c.run(ctx, "status")
	if err != nil {
		return Status{}, err
	}

	var status Status
	if err := json.Unmarshal(out, &status); err != nil {
		return Status{}, fmt.Errorf("parse bw status output: %s", describeNonJSON(out))
	}
	return status, nil
}

func (c *CLI) EnsureUnlocked(ctx context.Context, in io.Reader, promptOut io.Writer) error {
	status, err := c.Status(ctx)
	if err != nil {
		return err
	}
	switch status.Status {
	case "unlocked":
		return nil
	case "locked":
		session, err := c.Unlock(ctx, in, promptOut)
		if err != nil {
			return err
		}
		c.Session = session
		return nil
	default:
		return VaultLockedError{Status: status.Status}
	}
}

func (c CLI) Unlock(ctx context.Context, in io.Reader, promptOut io.Writer) (string, error) {
	out, err := c.runInteractive(ctx, in, promptOut, "unlock", "--raw")
	if err != nil {
		return "", err
	}
	session := strings.TrimSpace(string(out))
	if session == "" {
		return "", fmt.Errorf("bw unlock did not return a session token")
	}
	return session, nil
}

func (c CLI) ListItems(ctx context.Context) ([]Item, error) {
	out, err := c.run(ctx, "list", "items")
	if err != nil {
		return nil, err
	}

	var items []Item
	if err := json.Unmarshal(out, &items); err != nil {
		return nil, fmt.Errorf("parse bw list items output: %s", describeNonJSON(out))
	}
	return items, nil
}

func (c CLI) ListSSHKeyItems(ctx context.Context) ([]Item, error) {
	items, err := c.ListItems(ctx)
	if err != nil {
		return nil, err
	}
	sshItems := make([]Item, 0, len(items))
	for _, item := range items {
		if item.IsSSHKey() {
			sshItems = append(sshItems, item)
		}
	}
	return sshItems, nil
}

func (c CLI) run(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, c.Path, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Env = c.env()

	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return nil, fmt.Errorf("bw %s failed: %s", strings.Join(args, " "), safeErrorMessage(message))
	}
	return stdout.Bytes(), nil
}

func (c CLI) runInteractive(ctx context.Context, in io.Reader, stderr io.Writer, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, c.Path, args...)
	var stdout bytes.Buffer
	var stderrBuffer bytes.Buffer
	cmd.Stdin = in
	cmd.Stdout = &stdout
	if stderr != nil {
		cmd.Stderr = io.MultiWriter(stderr, &stderrBuffer)
	} else {
		cmd.Stderr = &stderrBuffer
	}
	cmd.Env = c.env()

	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(stderrBuffer.String())
		if message == "" {
			message = err.Error()
		}
		return nil, fmt.Errorf("bw %s failed: %s", strings.Join(args, " "), safeErrorMessage(message))
	}
	return stdout.Bytes(), nil
}

func (c CLI) env() []string {
	env := []string{}
	for _, value := range os.Environ() {
		if strings.HasPrefix(value, "BW_SESSION=") && strings.TrimSpace(c.Session) != "" {
			continue
		}
		env = append(env, value)
	}
	if strings.TrimSpace(c.Session) != "" {
		env = append(env, "BW_SESSION="+c.Session)
	}
	return env
}

func LooksLikeSessionError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "not logged in") ||
		strings.Contains(message, "not authenticated") ||
		strings.Contains(message, "unauthenticated") ||
		strings.Contains(message, "invalid session") ||
		strings.Contains(message, "locked") ||
		strings.Contains(message, "master password") ||
		strings.Contains(message, "empty output") ||
		strings.Contains(message, "bw_session")
}

func safeErrorMessage(message string) string {
	message = strings.ReplaceAll(message, "\n", " ")
	message = strings.TrimSpace(message)
	if len(message) > 300 {
		return message[:300] + "..."
	}
	return message
}

func describeNonJSON(output []byte) string {
	text := safeErrorMessage(string(output))
	if text == "" {
		return "empty output"
	}
	lower := strings.ToLower(text)
	if strings.Contains(lower, "master password") || strings.Contains(lower, "input is hidden") {
		return "bw asked for a master password; unlock first with `export BW_SESSION=\"$(bw unlock --raw)\"`"
	}
	return text
}
