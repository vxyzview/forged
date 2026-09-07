// Package tui implements the FORGED Bubble Tea user interface: a live build
// monitor (progress + scrolling colourised log viewport), a log-review screen
// and a final results screen.
//
// FORGED — Android Kernel Builder
// Copyright (c) 2026 vxyzview. Made with love.
package tui

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/vxyzview/forged/internal/builder"
)

// ── Palette (solid terminal colours, no gradients) ───────────────────────────

var (
	primary   = lipgloss.Color("#ff7c00")
	secondary = lipgloss.Color("#ffbe00")
	success   = lipgloss.Color("#39d353")
	fail      = lipgloss.Color("#ff4444")
	warning   = lipgloss.Color("#ff9f2f")
	dim       = lipgloss.Color("#606060")
	steel     = lipgloss.Color("#a8b2c0")
)

// ── Log colouriser ───────────────────────────────────────────────────────────

var (
	errorRe = regexp.MustCompile(`(?i)\berror\b`)
	warnRe  = regexp.MustCompile(`(?i)\bwarning\b`)
	noteRe  = regexp.MustCompile(`(?i)\bnote\b`)
	okRe    = regexp.MustCompile(`(?i)\b(OK|PASS|done|success)\b`)
	linkRe  = regexp.MustCompile(`\bLD\b|\bLINK\b`)
	ccRe    = regexp.MustCompile(`\b(CC|AS|AR|OBJCOPY|STRIP|NM)\b`)
	genRe   = regexp.MustCompile(`\b(GEN|INSTALL|HOSTCC|HOSTLD)\b`)
	makeRe  = regexp.MustCompile(`^\s*make\[`)
)

// phaseIcon maps build steps to a single quiet glyph — the step name carries
// the meaning; the mark only says "phase".
var phaseIcons = map[string]string{
	"mrproper":  "·",
	"defconfig": "·",
	"compile":   "·",
	"clone":     "·",
	"package":   "·",
	"setup":     "·",
}

func phaseIcon(name string) string {
	for key, icon := range phaseIcons {
		if strings.Contains(strings.ToLower(name), key) {
			return icon
		}
	}
	return "·"
}

// Colourise applies colour hints to a raw compiler/make output line. It is
// exported so the non-TTY fallback output looks identical to the TUI log.
func Colourise(line string) string {
	stripped := strings.TrimRight(line, "\r\n")
	if stripped == "" {
		return line
	}
	prefix := func(glyph, colour string) string {
		return lipgloss.NewStyle().Foreground(lipgloss.Color(colour)).Render(glyph) +
			lipgloss.NewStyle().Foreground(lipgloss.Color(colour)).Render(stripped)
	}
	switch {
	case errorRe.MatchString(line):
		return prefix("  ✗  ", "#ff4444")
	case warnRe.MatchString(line):
		return prefix("  ▲  ", "#ffbe00")
	case noteRe.MatchString(line):
		return prefix("  ›  ", "#a8b2c0")
	case okRe.MatchString(line):
		return prefix("  ✓  ", "#39d353")
	case linkRe.MatchString(line):
		return prefix("  ·  ", "#ff9f2f")
	case genRe.MatchString(line):
		return prefix("  ·  ", "#c8d0d8")
	case ccRe.MatchString(line):
		return prefix("  ·  ", "#d8e0e8")
	case makeRe.MatchString(line):
		return "     " + lipgloss.NewStyle().Foreground(dim).Render(stripped)
	default:
		return "     " + stripped
	}
}

// ── Messages ─────────────────────────────────────────────────────────────────

// LogLineMsg carries one streamed subprocess line into the TUI.
type LogLineMsg struct{ Line string }

// StepStartMsg announces the start of a build step.
type StepStartMsg struct {
	Name  string
	Index int
	Total int
}

// StepDoneMsg carries the finished result of a build step.
type StepDoneMsg struct{ Result builder.BuildResult }

// TickMsg drives the elapsed-time clock.
type TickMsg time.Time

// DoneMsg signals the runner goroutine finished all steps.
type DoneMsg struct{}

// quitSoonMsg is delivered ~1.5s after the final step completes; the model
// responds with tea.Quit so the TUI never strands the user on a final frame.
type quitSoonMsg struct{}

// ── Build state machine ──────────────────────────────────────────────────────

// BuildModel is the Bubble Tea model for the live build monitor.
type BuildModel struct {
	// configuration
	stepNames []string
	total     int

	// Messages is an optional inbound channel of runner messages. When set,
	// the model re-arms a wait command on every Update so messages are pulled
	// in through the standard Bubble Tea command pattern (no Send races).
	Messages <-chan tea.Msg

	// dynamic
	spinner   spinner.Model
	viewport  viewport.Model
	ready     bool
	width     int
	height    int
	logLines  []string // all lines ever received (untruncated)
	lineCount int
	current   string // current step name
	stepIndex int    // 1-based
	results   []builder.BuildResult
	startTime time.Time
	elapsed   time.Duration
	done      bool
	failed    bool
}

// NewBuildModel creates the live build monitor for the given step names.
func NewBuildModel(stepNames []string) BuildModel {
	sp := spinner.New(spinner.WithSpinner(spinner.Dot))
	return BuildModel{
		stepNames: stepNames,
		total:     len(stepNames),
		spinner:   sp,
		startTime: time.Now(),
	}
}

// WithMessages attaches the runner message channel.
func (m BuildModel) WithMessages(ch <-chan tea.Msg) BuildModel {
	m.Messages = ch
	return m
}

// waitMsg returns a command that pulls the next message from ch.
func waitMsg(ch <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return DoneMsg{}
		}
		return msg
	}
}

// Init starts the spinner, the clock ticker and the message pump.
func (m BuildModel) Init() tea.Cmd {
	cmds := []tea.Cmd{m.spinner.Tick, tickCmd()}
	if m.Messages != nil {
		cmds = append(cmds, waitMsg(m.Messages))
	}
	return tea.Batch(cmds...)
}

func tickCmd() tea.Cmd {
	return tea.Tick(200*time.Millisecond, func(t time.Time) tea.Msg {
		return TickMsg(t)
	})
}

// LineCount returns the number of captured log lines.
func (m BuildModel) LineCount() int { return m.lineCount }

// AllLines returns every captured log line in order.
func (m BuildModel) AllLines() []string { return append([]string(nil), m.logLines...) }

// Results returns per-step results collected so far.
func (m BuildModel) Results() []builder.BuildResult {
	return append([]builder.BuildResult(nil), m.results...)
}

// Done reports whether all steps finished.
func (m BuildModel) Done() bool { return m.done }

// Failed reports whether any step failed.
func (m BuildModel) Failed() bool { return m.failed }

// Elapsed returns the wall-clock build time.
func (m BuildModel) Elapsed() time.Duration { return m.elapsed }

// Update handles messages from the runner goroutine and the terminal.
func (m BuildModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		// Chrome is now flat: one header line + one progress line above the
		// log, the step list below — smaller reserve than the old boxed UI.
		headerHeight := 2
		footerHeight := 4
		logHeight := m.height - headerHeight - footerHeight
		if logHeight < 5 {
			logHeight = 5
		}
		if !m.ready {
			m.viewport = viewport.New(msg.Width, logHeight)
			m.viewport.SetContent("")
			m.ready = true
		} else {
			m.viewport.Width = msg.Width
			m.viewport.Height = logHeight
		}

	case TickMsg:
		m.elapsed = time.Since(m.startTime)
		if !m.done {
			cmds = append(cmds, tickCmd())
		}

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		cmds = append(cmds, cmd)

	case LogLineMsg:
		m.logLines = append(m.logLines, msg.Line)
		m.lineCount++
		if m.ready {
			m.viewport.SetContent(strings.Join(colourisedTail(m.logLines), "\n"))
			m.viewport.GotoBottom()
		}

	case StepStartMsg:
		m.current = msg.Name
		m.stepIndex = msg.Index + 1

	case StepDoneMsg:
		m.results = append(m.results, msg.Result)
		if !msg.Result.Success {
			m.failed = true
		}
		if len(m.results) >= m.total && m.total > 0 {
			m.done = true
			m.elapsed = time.Since(m.startTime)
			// Auto-quit shortly after the last step lands: the build is
			// finished, so the TUI hands control back to the summary that
			// follows. No more dead-end screens waiting for a keypress.
			cmds = append(cmds, tea.Tick(1500*time.Millisecond, func(time.Time) tea.Msg {
				return quitSoonMsg{}
			}))
		}

	case quitSoonMsg:
		return m, tea.Quit

	case DoneMsg:
		// Runner channel closed; nothing further to pull.

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		}
	}

	// Stop re-arming the pump once everything is done.
	if m.Messages != nil && !m.done {
		cmds = append(cmds, waitMsg(m.Messages))
	}

	if m.ready {
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

// colourisedTail returns the last N colourised lines for the viewport.
func colourisedTail(lines []string) []string {
	const window = 200
	start := 0
	if len(lines) > window {
		start = len(lines) - window
	}
	out := make([]string, 0, len(lines)-start)
	for _, l := range lines[start:] {
		out = append(out, Colourise(l))
	}
	return out
}

// View renders the whole screen: header, progress, log viewport, footer.
// Minimalist: three flat sections, no nested boxes, no decorative chrome.
func (m BuildModel) View() string {
	if m.width == 0 {
		return "  ◆  FORGED — preparing live build display…\n"
	}

	dimStyle := lipgloss.NewStyle().Foreground(dim)

	// ── Header: FORGED · STEP [n/N] · clock, one line ──
	stepLabel := m.current
	if stepLabel == "" && len(m.stepNames) > 0 {
		stepLabel = m.stepNames[0]
	}
	header := lipgloss.NewStyle().Bold(true).Foreground(primary).
		Render("FORGED") +
		dimStyle.Render("  ·  ") +
		lipgloss.NewStyle().Bold(true).Foreground(steel).
			Render(strings.ToUpper(stepLabel)) +
		dimStyle.Render(fmt.Sprintf("  [%d/%d]", m.stepIndex, m.total)) +
		dimStyle.Render("  "+formatDuration(m.elapsed)) +
		"  " + m.spinner.View()

	// ── Progress: thin single-row bar ──
	done := len(m.results)
	bar := progressBar(done, m.total, barInnerWidth(m.width))
	progress := "  " + bar + dimStyle.Render(fmt.Sprintf("  %d/%d", done, m.total))

	// ── Log viewport: bare, no surrounding box ──
	logPanel := dimStyle.Render("  waiting for output…")
	if m.ready {
		logPanel = m.viewport.View()
	}

	// ── Step results so far ──
	var resultLines []string
	for _, r := range m.results {
		if r.Success {
			resultLines = append(resultLines,
				lipgloss.NewStyle().Foreground(success).Render(
					fmt.Sprintf("  ✓  %s", r.Step))+
					dimStyle.Render(fmt.Sprintf("  %.2fs", r.Duration)))
		} else {
			resultLines = append(resultLines,
				lipgloss.NewStyle().Foreground(fail).Render(
					fmt.Sprintf("  ✗  %s  %s", r.Step, r.Error)))
		}
	}
	results := strings.Join(resultLines, "\n")

	return strings.Join([]string{header, progress, logPanel, results}, "\n")
}

func barInnerWidth(total int) int {
	w := total - 30
	if w < 10 {
		w = 10
	}
	return w
}

func progressBar(done, total, width int) string {
	filled := 0
	if total > 0 {
		filled = done * width / total
	}
	fill := lipgloss.NewStyle().Foreground(primary).Render(strings.Repeat("─", filled))
	empty := lipgloss.NewStyle().Foreground(lipgloss.Color("#3a3a3a")).Render(strings.Repeat("─", width-filled))
	return fill + empty
}

func formatDuration(d time.Duration) string {
	secs := int(d.Seconds())
	return fmt.Sprintf("%02d:%02d", secs/60, secs%60)
}

// ── Errors and warnings extraction ───────────────────────────────────────────

// Issues holds the error/warning lines extracted from a build log.
type Issues struct {
	Errors   []string
	Warnings []string
}

// ExtractIssues classifies log lines into errors and warnings.
// Lines matching both are classified as errors (higher severity wins).
func ExtractIssues(lines []string) Issues {
	var out Issues
	for _, l := range lines {
		switch {
		case errorRe.MatchString(l):
			out.Errors = append(out.Errors, l)
		case warnRe.MatchString(l):
			out.Warnings = append(out.Warnings, l)
		}
	}
	return out
}

// RenderResultsTable renders the final per-step summary as a static string.
func RenderResultsTable(results []builder.BuildResult, zipPath string) string {
	allOK := true
	total := 0.0
	for _, r := range results {
		total += r.Duration
		if !r.Success {
			allOK = false
		}
	}

	heading := "  ✓  ALL STEPS PASSED  "
	headingColour := success
	if !allOK {
		heading = "  ✗  BUILD FAILED  "
		headingColour = fail
	}

	title := lipgloss.NewStyle().Bold(true).Foreground(headingColour).
		Padding(0, 1).Render(heading)

	// Column padding is computed on the un-styled text: ANSI escapes would
	// otherwise skew %-14s width bookkeeping.
	rows := []string{"STEP            STATUS      TIME", strings.Repeat("─", 38)}
	for _, r := range results {
		status, mark := "pass", "✓"
		if !r.Success {
			status, mark = "fail", "✗"
		}
		rows = append(rows, fmt.Sprintf("%-14s  %s %s  %9.2fs",
			r.Step,
			lipgloss.NewStyle().Foreground(markColour(r.Success)).Render(mark),
			status, r.Duration))
	}
	rows = append(rows, strings.Repeat("─", 38), fmt.Sprintf("total                        %9.2fs", total))

	if zipPath != "" {
		rows = append(rows, "", lipgloss.NewStyle().Foreground(secondary).Render("→ "+zipPath))
	}

	body := strings.Join(rows, "\n")
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(headingColour).
		Padding(0, 2).
		Render(title + "\n\n" + body)
}

func markColour(ok bool) lipgloss.Color {
	if ok {
		return success
	}
	return fail
}
