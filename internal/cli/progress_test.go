package cli

import (
	"bytes"
	"testing"
)

func TestRenderProgress(t *testing.T) {
	got := renderProgress(3, 6, "Generating SSH config")
	want := "[##########----------] 3/6 Generating SSH config"
	if got != want {
		t.Fatalf("renderProgress() = %q, want %q", got, want)
	}
}

func TestRenderProgressClampsStep(t *testing.T) {
	got := renderProgress(9, 3, "Done")
	want := "[####################] 3/3 Done"
	if got != want {
		t.Fatalf("renderProgress() = %q, want %q", got, want)
	}
}

func TestTerminalProgressIsSilentForNonTerminalWriter(t *testing.T) {
	var output bytes.Buffer
	progress := newTerminalProgress(&output, 6, false)
	progress.Update(1, "Syncing Bitwarden")
	progress.Done("Applied")
	if output.Len() != 0 {
		t.Fatalf("non-terminal progress output = %q", output.String())
	}
}

func TestTerminalProgressSuspendAndResume(t *testing.T) {
	var output bytes.Buffer
	progress := &terminalProgress{out: &output, total: 2}
	progress.Update(1, "Syncing")
	progress.Suspend()
	progress.Resume()
	progress.Done("Applied")

	want := "\r\x1b[2K[##########----------] 1/2 Syncing" +
		"\r\x1b[2K" +
		"\r\x1b[2K[##########----------] 1/2 Syncing" +
		"\r\x1b[2K[####################] 2/2 Applied\n"
	if output.String() != want {
		t.Fatalf("progress output = %q, want %q", output.String(), want)
	}
}
