package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

const progressBarWidth = 20

type progressReporter interface {
	Update(step int, message string)
	Suspend()
	Resume()
}

type noopProgress struct{}

func (noopProgress) Update(int, string) {}
func (noopProgress) Suspend()           {}
func (noopProgress) Resume()            {}

type terminalProgress struct {
	out       io.Writer
	total     int
	step      int
	message   string
	visible   bool
	suspended bool
}

func newTerminalProgress(out io.Writer, total int, disabled bool) *terminalProgress {
	progress := &terminalProgress{out: out, total: total}
	if disabled || total <= 0 || !isTerminalOutput(out) {
		progress.out = nil
	}
	return progress
}

func (p *terminalProgress) Update(step int, message string) {
	if p == nil || p.out == nil {
		return
	}
	if step < 0 {
		step = 0
	}
	if step > p.total {
		step = p.total
	}
	p.step = step
	p.message = message
	if !p.suspended {
		p.draw()
	}
}

func (p *terminalProgress) Suspend() {
	if p == nil || p.out == nil || p.suspended {
		return
	}
	p.clearLine()
	p.suspended = true
}

func (p *terminalProgress) Resume() {
	if p == nil || p.out == nil || !p.suspended {
		return
	}
	p.suspended = false
	if p.message != "" {
		p.draw()
	}
}

func (p *terminalProgress) Done(message string) {
	if p == nil || p.out == nil {
		return
	}
	p.suspended = false
	p.step = p.total
	p.message = message
	p.draw()
	fmt.Fprintln(p.out)
	p.visible = false
}

func (p *terminalProgress) Clear() {
	if p == nil || p.out == nil {
		return
	}
	p.clearLine()
}

func (p *terminalProgress) draw() {
	fmt.Fprintf(p.out, "\r\x1b[2K%s", renderProgress(p.step, p.total, p.message))
	p.visible = true
}

func (p *terminalProgress) clearLine() {
	if p.visible {
		fmt.Fprint(p.out, "\r\x1b[2K")
		p.visible = false
	}
}

func renderProgress(step, total int, message string) string {
	if total <= 0 {
		return strings.TrimSpace(message)
	}
	if step < 0 {
		step = 0
	}
	if step > total {
		step = total
	}
	filled := progressBarWidth * step / total
	bar := strings.Repeat("#", filled) + strings.Repeat("-", progressBarWidth-filled)
	return fmt.Sprintf("[%s] %d/%d %s", bar, step, total, strings.TrimSpace(message))
}

func isTerminalOutput(out io.Writer) bool {
	if os.Getenv("TERM") == "dumb" {
		return false
	}
	file, ok := out.(*os.File)
	return ok && term.IsTerminal(int(file.Fd()))
}
